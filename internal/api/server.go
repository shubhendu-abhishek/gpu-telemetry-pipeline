package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/telemetry"
)
const ( DefaultLimit = 1000; MaxLimit = 5000 )
type Repository interface { ListGPUs(context.Context) ([]GPU,error); GPUExists(context.Context,string)(bool,error); GetTelemetry(context.Context,string,*time.Time,*time.Time,int,int)([]telemetry.Record,error); Ping(context.Context) error }
type Server struct { repo Repository; logger *slog.Logger; requests atomic.Uint64 }
func NewServer(repo Repository, logger *slog.Logger) (*Server,error) { if repo==nil { return nil,errors.New("API repository is required") }; if logger==nil { logger=slog.Default() }; return &Server{repo:repo,logger:logger},nil }
func(s *Server) Handler() http.Handler { mux:=http.NewServeMux(); mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,http.StatusOK,map[string]string{"status":"ok"})}); mux.HandleFunc("GET /readyz",s.ready); mux.HandleFunc("GET /metrics",s.metrics); mux.HandleFunc("GET /openapi.json",func(w http.ResponseWriter,_ *http.Request){writeJSON(w,http.StatusOK,OpenAPISpec())}); mux.HandleFunc("GET /swagger",func(w http.ResponseWriter,_ *http.Request){w.Header().Set("Content-Type","text/html; charset=utf-8"); _,_=w.Write([]byte(swaggerHTML))}); mux.HandleFunc("GET /api/v1/gpus",s.listGPUs); mux.HandleFunc("GET /api/v1/gpus/{id}/telemetry",s.getTelemetry); return s.instrument(mux) }
func(s *Server) instrument(next http.Handler) http.Handler { return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){s.requests.Add(1); next.ServeHTTP(w,r)}) }
func(s *Server) ready(w http.ResponseWriter,r *http.Request) { ctx,cancel:=context.WithTimeout(r.Context(),2*time.Second); defer cancel(); if err:=s.repo.Ping(ctx); err!=nil {writeError(w,http.StatusServiceUnavailable,"database is not ready");return}; writeJSON(w,http.StatusOK,map[string]string{"status":"ready"}) }
func(s *Server) metrics(w http.ResponseWriter,_ *http.Request) { w.Header().Set("Content-Type","text/plain; version=0.0.4; charset=utf-8"); _,_=fmt.Fprintf(w,"# HELP http_requests_total Total HTTP requests handled.\n# TYPE http_requests_total counter\nhttp_requests_total %d\n",s.requests.Load()) }
func(s *Server) listGPUs(w http.ResponseWriter,r *http.Request) { items,err:=s.repo.ListGPUs(r.Context()); if err!=nil {s.logger.Error("list GPUs failed","error",err);writeError(w,http.StatusInternalServerError,"failed to list GPUs");return}; if items==nil {items=[]GPU{}}; writeJSON(w,http.StatusOK,GPUListResponse{Items:items,Count:len(items)}) }
func(s *Server) getTelemetry(w http.ResponseWriter,r *http.Request) { id:=strings.TrimSpace(r.PathValue("id")); if id=="" {writeError(w,http.StatusBadRequest,"GPU id is required");return}; start,err:=parseOptionalTime(r.URL.Query().Get("start_time")); if err!=nil {writeError(w,http.StatusBadRequest,"start_time must be RFC3339: "+err.Error());return}; end,err:=parseOptionalTime(r.URL.Query().Get("end_time")); if err!=nil {writeError(w,http.StatusBadRequest,"end_time must be RFC3339: "+err.Error());return}; if start!=nil&&end!=nil&&end.Before(*start) {writeError(w,http.StatusBadRequest,"end_time must be greater than or equal to start_time");return}; limit:=DefaultLimit; offset:=0; if raw:=r.URL.Query().Get("limit");raw!="" {limit,err=strconv.Atoi(raw); if err!=nil||limit<1||limit>MaxLimit {writeError(w,http.StatusBadRequest,fmt.Sprintf("limit must be between 1 and %d",MaxLimit));return}}; if raw:=r.URL.Query().Get("offset");raw!="" {offset,err=strconv.Atoi(raw); if err!=nil||offset<0 {writeError(w,http.StatusBadRequest,"offset must be a non-negative integer");return}}; exists,err:=s.repo.GPUExists(r.Context(),id); if err!=nil {s.logger.Error("check GPU failed","gpu_uuid",id,"error",err);writeError(w,http.StatusInternalServerError,"failed to query GPU");return}; if !exists {writeError(w,http.StatusNotFound,"GPU not found");return}; items,err:=s.repo.GetTelemetry(r.Context(),id,start,end,limit,offset); if err!=nil {s.logger.Error("query telemetry failed","gpu_uuid",id,"error",err);writeError(w,http.StatusInternalServerError,"failed to query telemetry");return}; if items==nil {items=[]telemetry.Record{}}; writeJSON(w,http.StatusOK,TelemetryResponse{GPUUUID:id,Items:items,Count:len(items),Limit:limit,Offset:offset}) }
func parseOptionalTime(raw string)(*time.Time,error){if raw==""{return nil,nil}; parsed,err:=time.Parse(time.RFC3339,raw);if err!=nil{return nil,err};utc:=parsed.UTC();return &utc,nil}
func writeJSON(w http.ResponseWriter,status int,value any){w.Header().Set("Content-Type","application/json; charset=utf-8");w.WriteHeader(status);_=json.NewEncoder(w).Encode(value)}
func writeError(w http.ResponseWriter,status int,message string){writeJSON(w,status,ErrorResponse{Error:message})}
const swaggerHTML=`<!doctype html><html><head><meta charset="utf-8"><title>GPU Telemetry API</title><link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"></head><body><div id="swagger-ui"></div><script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:'/openapi.json',dom_id:'#swagger-ui'});</script></body></html>`
