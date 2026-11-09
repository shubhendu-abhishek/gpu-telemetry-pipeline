package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/telemetry"
)
type stubRepo struct { gpus []GPU; records []telemetry.Record; exists bool; err error; start,end *time.Time; limit,offset int }
func(s *stubRepo)ListGPUs(context.Context)([]GPU,error){return s.gpus,s.err}
func(s *stubRepo)GPUExists(context.Context,string)(bool,error){return s.exists,s.err}
func(s *stubRepo)GetTelemetry(_ context.Context,_ string,start,end *time.Time,limit,offset int)([]telemetry.Record,error){s.start=start;s.end=end;s.limit=limit;s.offset=offset;return s.records,s.err}
func(s *stubRepo)Ping(context.Context)error{return s.err}
func newTestHandler(t *testing.T,repo *stubRepo)http.Handler{t.Helper();s,err:=NewServer(repo,nil);if err!=nil{t.Fatal(err)};return s.Handler()}
func TestListGPUs(t *testing.T){repo:=&stubRepo{gpus:[]GPU{{UUID:"GPU-1"}}};w:=httptest.NewRecorder();newTestHandler(t,repo).ServeHTTP(w,httptest.NewRequest(http.MethodGet,"/api/v1/gpus",nil));if w.Code!=http.StatusOK||!strings.Contains(w.Body.String(),"GPU-1"){t.Fatalf("status=%d body=%s",w.Code,w.Body.String())}}
func TestListGPUsError(t *testing.T){repo:=&stubRepo{err:errors.New("db")};w:=httptest.NewRecorder();newTestHandler(t,repo).ServeHTTP(w,httptest.NewRequest(http.MethodGet,"/api/v1/gpus",nil));if w.Code!=http.StatusInternalServerError{t.Fatalf("status=%d",w.Code)}}
func TestGetTelemetryTimeBoundsAreInclusive(t *testing.T){start:=time.Date(2026,1,1,0,0,0,0,time.UTC);end:=start.Add(time.Hour);repo:=&stubRepo{exists:true,records:[]telemetry.Record{}};url:="/api/v1/gpus/GPU-1/telemetry?start_time="+start.Format(time.RFC3339)+"&end_time="+end.Format(time.RFC3339)+"&limit=20&offset=3";w:=httptest.NewRecorder();newTestHandler(t,repo).ServeHTTP(w,httptest.NewRequest(http.MethodGet,url,nil));if w.Code!=http.StatusOK{t.Fatalf("status=%d body=%s",w.Code,w.Body.String())};if repo.start==nil||!repo.start.Equal(start)||repo.end==nil||!repo.end.Equal(end)||repo.limit!=20||repo.offset!=3{t.Fatalf("unexpected query args: %+v",repo)}}
func TestGetTelemetryRejectsInvalidQuery(t *testing.T){cases:=[]string{"?start_time=bad","?end_time=bad","?start_time=2026-01-02T00:00:00Z&end_time=2026-01-01T00:00:00Z","?limit=0","?limit=5001","?offset=-1","?offset=abc"};for _,query:=range cases{t.Run(query,func(t *testing.T){repo:=&stubRepo{exists:true};w:=httptest.NewRecorder();newTestHandler(t,repo).ServeHTTP(w,httptest.NewRequest(http.MethodGet,"/api/v1/gpus/GPU-1/telemetry"+query,nil));if w.Code!=http.StatusBadRequest{t.Fatalf("status=%d body=%s",w.Code,w.Body.String())}})}}
func TestGetTelemetryNotFound(t *testing.T){w:=httptest.NewRecorder();newTestHandler(t,&stubRepo{}).ServeHTTP(w,httptest.NewRequest(http.MethodGet,"/api/v1/gpus/GPU-404/telemetry",nil));if w.Code!=http.StatusNotFound{t.Fatalf("status=%d",w.Code)}}
func TestGetTelemetryRepositoryErrors(t *testing.T){repo:=&stubRepo{exists:true,err:errors.New("db")};w:=httptest.NewRecorder();newTestHandler(t,repo).ServeHTTP(w,httptest.NewRequest(http.MethodGet,"/api/v1/gpus/GPU-1/telemetry",nil));if w.Code!=http.StatusInternalServerError{t.Fatalf("status=%d",w.Code)}}
func TestHealthAndReadiness(t *testing.T){h:=newTestHandler(t,&stubRepo{});for path,want:=range map[string]int{"/healthz":200,"/readyz":200,"/openapi.json":200,"/swagger":200,"/metrics":200}{w:=httptest.NewRecorder();h.ServeHTTP(w,httptest.NewRequest(http.MethodGet,path,nil));if w.Code!=want{t.Fatalf("%s status=%d",path,w.Code)}}}
func TestReadinessUnavailable(t *testing.T){repo:=&stubRepo{err:errors.New("db down")};w:=httptest.NewRecorder();newTestHandler(t,repo).ServeHTTP(w,httptest.NewRequest(http.MethodGet,"/readyz",nil));if w.Code!=http.StatusServiceUnavailable{t.Fatalf("status=%d",w.Code)}}
func TestParseOptionalTime(t *testing.T){if _,err:=parseOptionalTime("");err!=nil{t.Fatal(err)};if _,err:=parseOptionalTime("bad");err==nil{t.Fatal("expected parse error")};v,err:=parseOptionalTime("2026-01-01T01:00:00+01:00");if err!=nil||v.Location()!=time.UTC{t.Fatalf("v=%v err=%v",v,err)}}
