package e2e

import (
	"encoding/json"
	"fmt"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/api"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestPipelineEndToEnd(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1; run through make test-e2e")
	}
	baseURL := os.Getenv("API_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	var inventory api.GPUListResponse
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/api/v1/gpus")
		if err == nil && response.StatusCode == http.StatusOK {
			_ = json.NewDecoder(response.Body).Decode(&inventory)
			response.Body.Close()
			if inventory.Count > 0 {
				break
			}
		} else if response != nil {
			response.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	if inventory.Count == 0 {
		t.Fatal("no GPU telemetry became visible before timeout")
	}
	gpu := inventory.Items[0]
	url := fmt.Sprintf("%s/api/v1/gpus/%s/telemetry", baseURL, gpu.UUID)
	var result api.TelemetryResponse
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil && response.StatusCode == http.StatusOK {
			_ = json.NewDecoder(response.Body).Decode(&result)
			response.Body.Close()
			if result.Count > 0 {
				break
			}
		} else if response != nil {
			response.Body.Close()
		}
		time.Sleep(250 * time.Millisecond)
	}
	if result.Count == 0 {
		t.Fatalf("no telemetry returned for %s", gpu.UUID)
	}
	record := result.Items[0]
	if record.GPUUUID != gpu.UUID {
		t.Fatalf("response GPU %s does not match requested %s", record.GPUUUID, gpu.UUID)
	}
	if time.Since(record.ProcessedAt) > 5*time.Minute {
		t.Fatalf("processed_at is not a current processing timestamp: %s", record.ProcessedAt)
	}
	if record.StreamedAt.IsZero() {
		t.Fatal("streamed_at was not assigned by the Streamer")
	}
	if record.SourceTimestamp.IsZero() {
		t.Fatal("source_timestamp was not preserved")
	}
	filteredURL := fmt.Sprintf("%s?start_time=%s&end_time=%s", url, record.ProcessedAt.Format(time.RFC3339Nano), record.ProcessedAt.Format(time.RFC3339Nano))
	filteredResponse, err := client.Get(filteredURL)
	if err != nil {
		t.Fatal(err)
	}
	defer filteredResponse.Body.Close()
	if filteredResponse.StatusCode != http.StatusOK {
		t.Fatalf("inclusive window returned HTTP %d", filteredResponse.StatusCode)
	}
	var filtered api.TelemetryResponse
	if err := json.NewDecoder(filteredResponse.Body).Decode(&filtered); err != nil {
		t.Fatal(err)
	}
	if filtered.Count < 1 {
		t.Fatal("inclusive start_time/end_time did not return the boundary record")
	}
}
