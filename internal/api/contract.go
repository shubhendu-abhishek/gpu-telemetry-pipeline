package api

// OpenAPISpec is the single source of truth for the API contract. The
// openapi-gen command serializes this Go declaration to api/openapi.json.
func OpenAPISpec() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": "GPU Telemetry API", "version": "1.0.0", "description": "Read GPU inventory and timestamp-ordered DCGM telemetry."},
		"servers": []any{map[string]any{"url": "/"}},
		"paths": map[string]any{
			"/api/v1/gpus": map[string]any{"get": map[string]any{
				"summary": "List GPUs with telemetry", "operationId": "listGPUs", "responses": map[string]any{
					"200": map[string]any{"description": "GPU inventory", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/GPUListResponse"}}}},
					"500": errorResponse("Internal server error"),
				},
			}},
			"/api/v1/gpus/{id}/telemetry": map[string]any{"get": map[string]any{
				"summary": "Query telemetry for one GPU", "operationId": "getGPUTelemetry", "parameters": []any{
					map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}, "description": "Globally unique GPU UUID."},
					map[string]any{"name": "start_time", "in": "query", "required": false, "schema": map[string]any{"type": "string", "format": "date-time"}, "description": "Inclusive lower bound on processing timestamp."},
					map[string]any{"name": "end_time", "in": "query", "required": false, "schema": map[string]any{"type": "string", "format": "date-time"}, "description": "Inclusive upper bound on processing timestamp."},
					map[string]any{"name": "limit", "in": "query", "required": false, "schema": map[string]any{"type": "integer", "minimum": 1, "maximum": MaxLimit, "default": DefaultLimit}},
					map[string]any{"name": "offset", "in": "query", "required": false, "schema": map[string]any{"type": "integer", "minimum": 0, "default": 0}},
				},
				"responses": map[string]any{
					"200": map[string]any{"description": "GPU telemetry ordered by processed_at ascending", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/TelemetryResponse"}}}},
					"400": errorResponse("Invalid query parameters"), "404": errorResponse("GPU not found"), "500": errorResponse("Internal server error"),
				},
			}},
		},
		"components": map[string]any{"schemas": map[string]any{
			"GPU": map[string]any{"type": "object", "required": []string{"uuid", "gpu_index", "device", "model_name", "host_name", "first_seen", "last_seen"}, "properties": map[string]any{
				"uuid": map[string]any{"type": "string"}, "gpu_index": map[string]any{"type": "integer"}, "device": map[string]any{"type": "string"}, "model_name": map[string]any{"type": "string"}, "host_name": map[string]any{"type": "string"}, "first_seen": map[string]any{"type": "string", "format": "date-time"}, "last_seen": map[string]any{"type": "string", "format": "date-time"},
			}},
			"GPUListResponse": map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/GPU"}}, "count": map[string]any{"type": "integer"}}},
			"TelemetryRecord": map[string]any{"type": "object", "properties": map[string]any{
				"source_timestamp": map[string]any{"type": "string", "format": "date-time"}, "streamed_at": map[string]any{"type": "string", "format": "date-time"}, "processed_at": map[string]any{"type": "string", "format": "date-time"}, "metric_name": map[string]any{"type": "string"}, "gpu_index": map[string]any{"type": "integer"}, "device": map[string]any{"type": "string"}, "gpu_uuid": map[string]any{"type": "string"}, "model_name": map[string]any{"type": "string"}, "host_name": map[string]any{"type": "string"}, "container": map[string]any{"type": "string"}, "pod": map[string]any{"type": "string"}, "namespace": map[string]any{"type": "string"}, "value": map[string]any{"type": "number"}, "labels_raw": map[string]any{"type": "string"},
			}},
			"TelemetryResponse": map[string]any{"type": "object", "properties": map[string]any{"gpu_uuid": map[string]any{"type": "string"}, "items": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/TelemetryRecord"}}, "count": map[string]any{"type": "integer"}, "limit": map[string]any{"type": "integer"}, "offset": map[string]any{"type": "integer"}}},
			"ErrorResponse":     map[string]any{"type": "object", "properties": map[string]any{"error": map[string]any{"type": "string"}}},
		}},
	}
}

func errorResponse(description string) map[string]any {
	return map[string]any{"description": description, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"}}}}
}
