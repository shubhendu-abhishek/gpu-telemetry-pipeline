package api

import (
	"time"

	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/telemetry"
)

type GPU struct {
	UUID      string    `json:"uuid"`
	GPUIndex  int       `json:"gpu_index"`
	Device    string    `json:"device"`
	ModelName string    `json:"model_name"`
	HostName  string    `json:"host_name"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

type GPUListResponse struct {
	Items []GPU `json:"items"`
	Count int   `json:"count"`
}
type TelemetryResponse struct {
	GPUUUID string             `json:"gpu_uuid"`
	Items   []telemetry.Record `json:"items"`
	Count   int                `json:"count"`
	Limit   int                `json:"limit"`
	Offset  int                `json:"offset"`
}
type ErrorResponse struct {
	Error string `json:"error"`
}
