package postgres

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/api"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/queue"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/telemetry"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS
var ErrLeaseLost = errors.New("message lease is no longer owned by this consumer")
var ErrGPUNotFound = errors.New("GPU not found")

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return &Store{pool: pool}, nil
}
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	if _, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	for _, name := range entries {
		contents, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(88274103)`); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("lock migrations: %w", err)
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if !exists {
			if _, err = tx.Exec(ctx, string(contents)); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("apply migration %s: %w", name, err)
			}
			if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, name); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}
func (s *Store) PublishNext(ctx context.Context, sourceID string, headerOffset int64, read queue.OffsetReader) (bool, error) {
	if sourceID == "" || read == nil {
		return false, errors.New("sourceID and reader are required")
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `INSERT INTO source_cursors(source_id,next_offset,header_offset) VALUES($1,$2,$2) ON CONFLICT(source_id) DO NOTHING`, sourceID, headerOffset); err != nil {
		return false, err
	}
	var offset int64
	if err = tx.QueryRow(ctx, `SELECT next_offset FROM source_cursors WHERE source_id=$1 FOR UPDATE`, sourceID).Scan(&offset); err != nil {
		return false, err
	}
	payload, nextOffset, eof, err := read(offset)
	if err != nil {
		return false, err
	}
	if eof {
		payload, nextOffset, eof, err = read(headerOffset)
		if err != nil {
			return false, err
		}
		if eof {
			return false, errors.New("CSV source has no data rows")
		}
	}
	id, err := newID()
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO queue_messages(id,payload) VALUES($1,$2::jsonb)`, id, string(payload)); err != nil {
		return false, fmt.Errorf("enqueue telemetry: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE source_cursors SET next_offset=$2,header_offset=$3,updated_at=NOW() WHERE source_id=$1`, sourceID, nextOffset, headerOffset); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
func (s *Store) Claim(ctx context.Context, consumerID string, limit int, lease time.Duration) ([]queue.Message, error) {
	rows, err := s.pool.Query(ctx, `WITH picked AS (SELECT id FROM queue_messages WHERE (status='ready' AND available_at<=NOW()) OR (status='inflight' AND lease_until<NOW()) ORDER BY created_at,id LIMIT $1 FOR UPDATE SKIP LOCKED) UPDATE queue_messages q SET status='inflight',consumer_id=$2,lease_until=NOW()+($3::bigint * INTERVAL '1 microsecond'),attempts=q.attempts+1,updated_at=NOW() FROM picked WHERE q.id=picked.id RETURNING q.id,q.payload,q.attempts,q.created_at`, limit, consumerID, lease.Microseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]queue.Message, 0, limit)
	for rows.Next() {
		var m queue.Message
		if err := rows.Scan(&m.ID, &m.Payload, &m.Attempts, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}
func (s *Store) Ack(ctx context.Context, id, consumerID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM queue_messages WHERE id=$1 AND status='inflight' AND consumer_id=$2`, id, consumerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}
func (s *Store) Nack(ctx context.Context, id, consumerID string, delay time.Duration, maxAttempts int, lastError string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE queue_messages SET status=CASE WHEN attempts >= $4 THEN 'dead' ELSE 'ready' END,available_at=NOW()+($3::bigint * INTERVAL '1 microsecond'),lease_until=NULL,consumer_id=NULL,last_error=$5,updated_at=NOW() WHERE id=$1 AND status='inflight' AND consumer_id=$2`, id, consumerID, delay.Microseconds(), maxAttempts, lastError)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}
func (s *Store) SaveTelemetry(ctx context.Context, messageID string, r telemetry.Record) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(ctx, `INSERT INTO gpu_inventory(uuid,gpu_index,device,model_name,host_name,first_seen,last_seen,container_name,pod_name,namespace_name) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),NULLIF($10,'')) ON CONFLICT(uuid) DO UPDATE SET gpu_index=EXCLUDED.gpu_index,device=EXCLUDED.device,model_name=EXCLUDED.model_name,host_name=EXCLUDED.host_name,first_seen=LEAST(gpu_inventory.first_seen,EXCLUDED.first_seen),last_seen=GREATEST(gpu_inventory.last_seen,EXCLUDED.last_seen),container_name=COALESCE(EXCLUDED.container_name,gpu_inventory.container_name),pod_name=COALESCE(EXCLUDED.pod_name,gpu_inventory.pod_name),namespace_name=COALESCE(EXCLUDED.namespace_name,gpu_inventory.namespace_name),updated_at=NOW()`, r.GPUUUID, r.GPUIndex, r.Device, r.ModelName, r.HostName, r.ProcessedAt, r.ProcessedAt, r.Container, r.Pod, r.Namespace)
	if err != nil {
		return fmt.Errorf("upsert GPU inventory: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO telemetry_records(message_id,source_timestamp,streamed_at,processed_at,metric_name,gpu_uuid,gpu_index,device,model_name,host_name,container_name,pod_name,namespace_name,value,labels_raw) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),$14,$15) ON CONFLICT(message_id) DO NOTHING`, messageID, r.SourceTimestamp, r.StreamedAt, r.ProcessedAt, r.MetricName, r.GPUUUID, r.GPUIndex, r.Device, r.ModelName, r.HostName, r.Container, r.Pod, r.Namespace, r.Value, r.LabelsRaw)
	if err != nil {
		return fmt.Errorf("insert telemetry record: %w", err)
	}
	return tx.Commit(ctx)
}
func (s *Store) ListGPUs(ctx context.Context) ([]api.GPU, error) {
	rows, err := s.pool.Query(ctx, `SELECT uuid,gpu_index,device,model_name,host_name,first_seen,last_seen FROM gpu_inventory ORDER BY host_name,gpu_index,uuid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]api.GPU, 0)
	for rows.Next() {
		var gpu api.GPU
		if err := rows.Scan(&gpu.UUID, &gpu.GPUIndex, &gpu.Device, &gpu.ModelName, &gpu.HostName, &gpu.FirstSeen, &gpu.LastSeen); err != nil {
			return nil, err
		}
		items = append(items, gpu)
	}
	return items, rows.Err()
}
func (s *Store) GPUExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gpu_inventory WHERE uuid=$1)`, id).Scan(&exists)
	return exists, err
}
func (s *Store) GetTelemetry(ctx context.Context, id string, start, end *time.Time, limit, offset int) ([]telemetry.Record, error) {
	rows, err := s.pool.Query(ctx, `SELECT source_timestamp,streamed_at,processed_at,metric_name,gpu_index,device,gpu_uuid,model_name,host_name,COALESCE(container_name,''),COALESCE(pod_name,''),COALESCE(namespace_name,''),value,labels_raw FROM telemetry_records WHERE gpu_uuid=$1 AND ($2::timestamptz IS NULL OR processed_at >= $2) AND ($3::timestamptz IS NULL OR processed_at <= $3) ORDER BY processed_at ASC,id ASC LIMIT $4 OFFSET $5`, id, start, end, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]telemetry.Record, 0)
	for rows.Next() {
		var r telemetry.Record
		if err := rows.Scan(&r.SourceTimestamp, &r.StreamedAt, &r.ProcessedAt, &r.MetricName, &r.GPUIndex, &r.Device, &r.GPUUUID, &r.ModelName, &r.HostName, &r.Container, &r.Pod, &r.Namespace, &r.Value, &r.LabelsRaw); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}
func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded), nil
}
