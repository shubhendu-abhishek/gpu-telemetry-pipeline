CREATE TABLE IF NOT EXISTS source_cursors (
    source_id TEXT PRIMARY KEY,
    next_offset BIGINT NOT NULL CHECK (next_offset >= 0),
    header_offset BIGINT NOT NULL CHECK (header_offset >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS queue_messages (
    id TEXT PRIMARY KEY,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'inflight', 'dead')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_until TIMESTAMPTZ,
    consumer_id TEXT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_queue_ready ON queue_messages (available_at, created_at) WHERE status = 'ready';
CREATE INDEX IF NOT EXISTS idx_queue_expired_lease ON queue_messages (lease_until, created_at) WHERE status = 'inflight';
CREATE INDEX IF NOT EXISTS idx_queue_dead_created ON queue_messages (created_at) WHERE status = 'dead';

CREATE TABLE IF NOT EXISTS gpu_inventory (
    uuid TEXT PRIMARY KEY,
    gpu_index INTEGER NOT NULL,
    device TEXT NOT NULL,
    model_name TEXT NOT NULL,
    host_name TEXT NOT NULL,
    first_seen TIMESTAMPTZ NOT NULL,
    last_seen TIMESTAMPTZ NOT NULL,
    container_name TEXT,
    pod_name TEXT,
    namespace_name TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_gpu_host ON gpu_inventory (host_name, gpu_index);

CREATE TABLE IF NOT EXISTS telemetry_records (
    id BIGSERIAL PRIMARY KEY,
    message_id TEXT NOT NULL UNIQUE,
    source_timestamp TIMESTAMPTZ NOT NULL,
    streamed_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL,
    metric_name TEXT NOT NULL,
    gpu_uuid TEXT NOT NULL REFERENCES gpu_inventory(uuid),
    gpu_index INTEGER NOT NULL,
    device TEXT NOT NULL,
    model_name TEXT NOT NULL,
    host_name TEXT NOT NULL,
    container_name TEXT,
    pod_name TEXT,
    namespace_name TEXT,
    value DOUBLE PRECISION NOT NULL,
    labels_raw TEXT NOT NULL,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_telemetry_gpu_time ON telemetry_records (gpu_uuid, processed_at, id);
CREATE INDEX IF NOT EXISTS idx_telemetry_metric_time ON telemetry_records (metric_name, processed_at);
