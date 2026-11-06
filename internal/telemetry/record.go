package telemetry

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Record is one independent DCGM metric datapoint. StreamedAt is assigned by
// the Streamer; ProcessedAt is assigned by the Collector when it processes it.
type Record struct {
	SourceTimestamp time.Time `json:"source_timestamp"`
	StreamedAt      time.Time `json:"streamed_at"`
	ProcessedAt     time.Time `json:"processed_at"`
	MetricName      string    `json:"metric_name"`
	GPUIndex        int       `json:"gpu_index"`
	Device          string    `json:"device"`
	GPUUUID         string    `json:"gpu_uuid"`
	ModelName       string    `json:"model_name"`
	HostName        string    `json:"host_name"`
	Container       string    `json:"container,omitempty"`
	Pod             string    `json:"pod,omitempty"`
	Namespace       string    `json:"namespace,omitempty"`
	Value           float64   `json:"value"`
	LabelsRaw       string    `json:"labels_raw"`
}

func ParseRow(headers, values []string, streamedAt time.Time) (Record, error) {
	if len(headers) != len(values) {
		return Record{}, fmt.Errorf("CSV row has %d columns; expected %d", len(values), len(headers))
	}
	fields := make(map[string]string, len(headers))
	for i, header := range headers {
		fields[strings.TrimSpace(header)] = strings.TrimSpace(values[i])
	}
	required := []string{"timestamp", "metric_name", "gpu_id", "device", "uuid", "modelName", "Hostname", "value", "labels_raw"}
	for _, key := range required {
		if fields[key] == "" {
			return Record{}, fmt.Errorf("required CSV field %q is empty", key)
		}
	}
	sourceTime, err := time.Parse(time.RFC3339Nano, fields["timestamp"])
	if err != nil {
		return Record{}, fmt.Errorf("parse timestamp: %w", err)
	}
	gpuIndex, err := strconv.Atoi(fields["gpu_id"])
	if err != nil || gpuIndex < 0 {
		return Record{}, fmt.Errorf("gpu_id must be a non-negative integer: %q", fields["gpu_id"])
	}
	value, err := strconv.ParseFloat(fields["value"], 64)
	if err != nil {
		return Record{}, fmt.Errorf("value must be numeric: %q", fields["value"])
	}
	if streamedAt.IsZero() {
		return Record{}, fmt.Errorf("streamedAt must not be zero")
	}
	return Record{
		SourceTimestamp: sourceTime.UTC(), StreamedAt: streamedAt.UTC(),
		MetricName: fields["metric_name"], GPUIndex: gpuIndex, Device: fields["device"],
		GPUUUID: fields["uuid"], ModelName: fields["modelName"], HostName: fields["Hostname"],
		Container: fields["container"], Pod: fields["pod"], Namespace: fields["namespace"],
		Value: value, LabelsRaw: fields["labels_raw"],
	}, nil
}
