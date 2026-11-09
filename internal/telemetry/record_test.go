package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testHeaders() []string {
	return []string{"timestamp", "metric_name", "gpu_id", "device", "uuid", "modelName", "Hostname", "container", "pod", "namespace", "value", "labels_raw"}
}
func testValues() []string {
	return []string{"2025-07-18T20:42:34Z", "DCGM_FI_DEV_GPU_UTIL", "2", "nvidia2", "GPU-abc", "NVIDIA H100", "host-a", "", "", "", "98", "driver=535"}
}
func TestParseRow(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.FixedZone("IST", 19800))
	record, err := ParseRow(testHeaders(), testValues(), now)
	if err != nil {
		t.Fatal(err)
	}
	if record.GPUUUID != "GPU-abc" || record.GPUIndex != 2 || record.Value != 98 {
		t.Fatalf("unexpected record: %+v", record)
	}
	if record.StreamedAt.Location() != time.UTC {
		t.Fatal("streamed timestamp is not normalized to UTC")
	}
	if !record.ProcessedAt.IsZero() {
		t.Fatal("processed timestamp should be assigned by Collector")
	}
	if record.SourceTimestamp.Location() != time.UTC {
		t.Fatal("source timestamp is not normalized to UTC")
	}
	if record.Pod != "" || record.Container != "" || record.Namespace != "" {
		t.Fatal("optional empty fields should remain empty")
	}
}
func TestParseRowRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]string)
	}{{"column count", func(v []string) { v = v[:len(v)-1] }}, {"missing uuid", func(v []string) { v[4] = "" }}, {"bad timestamp", func(v []string) { v[0] = "yesterday" }}, {"bad gpu index", func(v []string) { v[2] = "gpu-2" }}, {"negative gpu index", func(v []string) { v[2] = "-1" }}, {"bad value", func(v []string) { v[10] = "high" }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := testValues()
			if tc.name == "column count" {
				values = values[:len(values)-1]
			} else {
				tc.mutate(values)
			}
			if _, err := ParseRow(testHeaders(), values, time.Now()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if _, err := ParseRow(testHeaders(), testValues(), time.Time{}); err == nil {
		t.Fatal("expected zero streamedAt error")
	}
}
func TestOpenCSVSourceAndReadAt(t *testing.T) {
	data := "timestamp,metric_name,gpu_id,device,uuid,modelName,Hostname,container,pod,namespace,value,labels_raw\n2025-07-18T20:42:34Z,DCGM_FI_DEV_GPU_UTIL,0,nvidia0,GPU-abc,H100,host-a,,,,50,labels\n2025-07-18T20:42:35Z,DCGM_FI_DEV_GPU_TEMP,0,nvidia0,GPU-abc,H100,host-a,,,,60,labels\n"
	file := t.TempDir() + "/input.csv"
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := OpenCSVSource(file)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	payload, next, eof, err := source.ReadAt(source.HeaderOffset(), time.Now().UTC())
	if err != nil || eof || len(payload) == 0 || next <= source.HeaderOffset() {
		t.Fatalf("read failed: eof=%v next=%d err=%v", eof, next, err)
	}
	_, next2, eof, err := source.ReadAt(next, time.Now().UTC())
	if err != nil || eof || next2 <= next {
		t.Fatalf("second read failed: eof=%v next=%d err=%v", eof, next2, err)
	}
	_, _, eof, err = source.ReadAt(next2, time.Now().UTC())
	if err != nil || !eof {
		t.Fatalf("expected EOF, got eof=%v err=%v", eof, err)
	}
}
func TestOpenCSVSourceRejectsMissingUUIDColumn(t *testing.T) {
	file := t.TempDir() + "/bad.csv"
	if err := os.WriteFile(file, []byte("timestamp,metric_name\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCSVSource(file); err == nil {
		t.Fatal("expected missing uuid error")
	}
}
func TestRequiredHeaders(t *testing.T) {
	got := strings.Join(RequiredHeaders(), ",")
	if got != "Hostname,device,gpu_id,labels_raw,metric_name,modelName,timestamp,uuid,value" {
		t.Fatalf("unexpected headers: %s", got)
	}
}
func TestCSVSourceAccessorsAndReadErrors(t *testing.T) {
	if _, err := OpenCSVSource(t.TempDir() + "/missing.csv"); err == nil {
		t.Fatal("expected open error")
	}
	empty := t.TempDir() + "/empty.csv"
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCSVSource(empty); err == nil {
		t.Fatal("expected empty CSV error")
	}
	badHeader := t.TempDir() + "/bad-header.csv"
	if err := os.WriteFile(badHeader, []byte("\"unterminated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCSVSource(badHeader); err == nil {
		t.Fatal("expected CSV header parse error")
	}
	badData := t.TempDir() + "/bad-data.csv"
	data := "timestamp,metric_name,gpu_id,device,uuid,modelName,Hostname,container,pod,namespace,value,labels_raw\n2025-07-18T20:42:34Z,,0,nvidia0,GPU-abc,H100,host-a,,,,50,labels\n"
	if err := os.WriteFile(badData, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := OpenCSVSource(badData)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if source.Path() != badData || len(source.Headers()) != 12 {
		t.Fatalf("unexpected source metadata: %s %v", source.Path(), source.Headers())
	}
	if _, _, _, err := source.ReadAt(source.HeaderOffset()-1, time.Now()); err == nil {
		t.Fatal("expected invalid offset error")
	}
	if _, _, _, err := source.ReadAt(source.HeaderOffset(), time.Now()); err == nil {
		t.Fatal("expected required field validation error")
	}
}
func TestCSVSourceRejectsWrongColumnCount(t *testing.T) {
	path := t.TempDir() + "/short-row.csv"
	data := "timestamp,metric_name,gpu_id,device,uuid,modelName,Hostname,container,pod,namespace,value,labels_raw\n2025-07-18T20:42:34Z,UTIL\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := OpenCSVSource(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, _, _, err := source.ReadAt(source.HeaderOffset(), time.Now()); err == nil {
		t.Fatal("expected CSV column count error")
	}
}


func TestBundledCSVFixtureHasOneValidDatapointPerRow(t *testing.T) {
	path := filepath.Join("..", "..", "data", "dcgm_metrics_20250718_134233.csv")
	source, err := OpenCSVSource(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	offset := source.HeaderOffset()
	count := 0
	for {
		payload, nextOffset, eof, err := source.ReadAt(offset, time.Now().UTC())
		if err != nil {
			t.Fatalf("read fixture at byte offset %d: %v", offset, err)
		}
		if eof {
			break
		}
		if len(payload) == 0 || nextOffset <= offset {
			t.Fatalf("invalid datapoint at byte offset %d: next=%d payload=%d", offset, nextOffset, len(payload))
		}
		offset = nextOffset
		count++
	}
	const expectedDatapoints = 2470
	if count != expectedDatapoints {
		t.Fatalf("expected %d independent telemetry datapoints, got %d", expectedDatapoints, count)
	}
}
