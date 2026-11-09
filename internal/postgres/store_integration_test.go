package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/telemetry"
)

func integrationStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; PostgreSQL integration test skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func TestPostgresQueueAndTelemetryLifecycle(t *testing.T) {
	store := integrationStore(t)
	ctx := context.Background()
	csv := t.TempDir() + "/telemetry.csv"
	data := "timestamp,metric_name,gpu_id,device,uuid,modelName,Hostname,container,pod,namespace,value,labels_raw\n" +
		"2025-07-18T20:42:34Z,DCGM_FI_DEV_GPU_UTIL,0,nvidia0,GPU-integration,H100,host-test,,,,51,labels\n" +
		"2025-07-18T20:42:35Z,DCGM_FI_DEV_GPU_TEMP,0,nvidia0,GPU-integration,H100,host-test,,,,60,labels\n"
	if err := os.WriteFile(csv, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := telemetry.OpenCSVSource(csv)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	sourceID := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.PublishNext(ctx, sourceID, source.HeaderOffset(), func(offset int64) ([]byte, int64, bool, error) {
				return source.ReadAt(offset, time.Now().UTC())
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	messages, err := store.Claim(ctx, "integration-consumer", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected two unique messages, got %d", len(messages))
	}
	gpuUUID := ""
	for _, message := range messages {
		var record telemetry.Record
		if err := json.Unmarshal(message.Payload, &record); err != nil {
			t.Fatal(err)
		}
		gpuUUID = record.GPUUUID
		record.ProcessedAt = time.Now().UTC()
		if err := store.SaveTelemetry(ctx, message.ID, record); err != nil {
			t.Fatal(err)
		}
		if err := store.Ack(ctx, message.ID, "integration-consumer"); err != nil {
			t.Fatal(err)
		}
	}
	if gpuUUID != "GPU-integration" {
		t.Fatalf("unexpected GPU UUID %q", gpuUUID)
	}
	gpus, err := store.ListGPUs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, gpu := range gpus {
		if gpu.UUID == gpuUUID {
			found = true
		}
	}
	if !found {
		t.Fatalf("GPU %s not found in inventory", gpuUUID)
	}
	exists, err := store.GPUExists(ctx, gpuUUID)
	if err != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(time.Hour)
	records, err := store.GetTelemetry(ctx, gpuUUID, &start, &end, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected two telemetry records, got %d", len(records))
	}
	if records[0].ProcessedAt.After(records[1].ProcessedAt) {
		t.Fatal("telemetry is not ordered by processing time")
	}
	if _, err := store.Claim(ctx, "integration-consumer", 10, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM telemetry_records WHERE gpu_uuid=$1`, gpuUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM gpu_inventory WHERE uuid=$1`, gpuUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM source_cursors WHERE source_id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresNackAndLeaseValidation(t *testing.T) {
	store := integrationStore(t)
	ctx := context.Background()
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO queue_messages(id,payload) VALUES($1,$2::jsonb)`, id, `{"gpu_uuid":"GPU-x"}`); err != nil {
		t.Fatal(err)
	}
	messages, err := store.Claim(ctx, "consumer-a", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("claimed %d", len(messages))
	}
	if err := store.Nack(ctx, id, "consumer-a", 0, 3, "temporary failure"); err != nil {
		t.Fatal(err)
	}
	messages, err = store.Claim(ctx, "consumer-b", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Attempts != 2 {
		t.Fatalf("unexpected retry: %+v", messages)
	}
	if err := store.Nack(ctx, id, "consumer-b", 0, 2, "max attempts reached"); err != nil {
		t.Fatal(err)
	}
	messages, err = store.Claim(ctx, "consumer-c", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("dead letter was claimed: %+v", messages)
	}
	if err := store.Ack(ctx, id, "consumer-a"); err != ErrLeaseLost {
		t.Fatalf("expected lease lost, got %v", err)
	}
	_, _ = store.pool.Exec(ctx, `DELETE FROM queue_messages WHERE id=$1`, id)
}

func TestPostgresConcurrentCollectorsClaimDisjointMessages(t *testing.T) {
	store := integrationStore(t)
	ctx := context.Background()

	const messageCount = 8
	ids := make([]string, 0, messageCount)
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = store.pool.Exec(context.Background(), `DELETE FROM queue_messages WHERE id=$1`, id)
		}
	})

	for i := 0; i < messageCount; i++ {
		id, err := newID()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO queue_messages(id,payload) VALUES($1,$2::jsonb)`, id, `{"test":true}`); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	type claimResult struct {
		messages int
		ids      []string
		err      error
	}
	results := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		consumerID := fmt.Sprintf("concurrent-collector-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			messages, err := store.Claim(ctx, consumerID, messageCount/2, time.Minute)
			result := claimResult{messages: len(messages), err: err}
			for _, message := range messages {
				result.ids = append(result.ids, message.ID)
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)

	claimed := make(map[string]struct{}, messageCount)
	total := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		total += result.messages
		for _, id := range result.ids {
			if _, duplicate := claimed[id]; duplicate {
				t.Fatalf("message %s was claimed by more than one collector", id)
			}
			claimed[id] = struct{}{}
		}
	}
	if total != messageCount {
		t.Fatalf("expected %d messages claimed across collectors, got %d", messageCount, total)
	}
	if len(claimed) != messageCount {
		t.Fatalf("expected %d unique claimed messages, got %d", messageCount, len(claimed))
	}
}

func TestPostgresExpiredLeaseCanBeReclaimedByAnotherCollector(t *testing.T) {
	store := integrationStore(t)
	ctx := context.Background()

	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM queue_messages WHERE id=$1`, id)
	})
	if _, err := store.pool.Exec(ctx, `INSERT INTO queue_messages(id,payload) VALUES($1,$2::jsonb)`, id, `{"test":true}`); err != nil {
		t.Fatal(err)
	}

	first, err := store.Claim(ctx, "collector-before-expiry", 1, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ID != id {
		t.Fatalf("expected the first collector to claim message %s, got %+v", id, first)
	}

	time.Sleep(250 * time.Millisecond)

	second, err := store.Claim(ctx, "collector-after-expiry", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].ID != id {
		t.Fatalf("expected the second collector to reclaim expired message %s, got %+v", id, second)
	}
	if second[0].Attempts != 2 {
		t.Fatalf("expected reclaim attempt count 2, got %d", second[0].Attempts)
	}
	if err := store.Ack(ctx, id, "collector-before-expiry"); err != ErrLeaseLost {
		t.Fatalf("expected old collector acknowledgement to fail with ErrLeaseLost, got %v", err)
	}
	if err := store.Ack(ctx, id, "collector-after-expiry"); err != nil {
		t.Fatalf("new lease owner could not acknowledge message: %v", err)
	}
}
