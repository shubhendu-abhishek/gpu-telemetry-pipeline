package telemetry

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// CSVSource reads individual CSV records at byte offsets. ReadAt uses ReaderAt
// semantics so multiple Streamer replicas can safely read the same file while
// the database cursor serializes offset allocation.
type CSVSource struct {
	file         *os.File
	path         string
	headers      []string
	headerOffset int64
}

func OpenCSVSource(path string) (*CSVSource, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open CSV %q: %w", path, err)
	}
	reader := csv.NewReader(file)
	headers, err := reader.Read()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	if len(headers) == 0 {
		file.Close()
		return nil, errors.New("CSV header is empty")
	}
	for i := range headers {
		headers[i] = strings.TrimSpace(headers[i])
	}
	for _, required := range RequiredHeaders() {
		if _, ok := headerIndex(headers, required); !ok {
			file.Close()
			return nil, fmt.Errorf("CSV is missing required %q column", required)
		}
	}
	return &CSVSource{file: file, path: path, headers: headers, headerOffset: reader.InputOffset()}, nil
}

func (s *CSVSource) Close() error        { return s.file.Close() }
func (s *CSVSource) Path() string        { return s.path }
func (s *CSVSource) HeaderOffset() int64 { return s.headerOffset }
func (s *CSVSource) Headers() []string   { return append([]string(nil), s.headers...) }

// ReadAt returns the JSON message payload and the next byte offset. eof is true
// only when no CSV records remain from the supplied offset.
func (s *CSVSource) ReadAt(offset int64, streamedAt time.Time) (payload []byte, nextOffset int64, eof bool, err error) {
	if offset < s.headerOffset {
		return nil, offset, false, fmt.Errorf("offset %d is before first data row %d", offset, s.headerOffset)
	}
	section := io.NewSectionReader(s.file, offset, 1<<62)
	reader := csv.NewReader(section)
	reader.FieldsPerRecord = len(s.headers)
	values, readErr := reader.Read()
	if errors.Is(readErr, io.EOF) {
		return nil, offset, true, nil
	}
	if readErr != nil {
		return nil, offset, false, fmt.Errorf("read CSV at offset %d: %w", offset, readErr)
	}
	record, parseErr := ParseRow(s.headers, values, streamedAt)
	if parseErr != nil {
		return nil, offset, false, parseErr
	}
	encoded, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		return nil, offset, false, fmt.Errorf("marshal telemetry: %w", marshalErr)
	}
	return encoded, offset + reader.InputOffset(), false, nil
}

func headerIndex(headers []string, name string) (int, bool) {
	for i, header := range headers {
		if header == name {
			return i, true
		}
	}
	return 0, false
}

// RequiredHeaders returns a stable copy useful for diagnostics and tests.
func RequiredHeaders() []string {
	out := []string{"timestamp", "metric_name", "gpu_id", "device", "uuid", "modelName", "Hostname", "value", "labels_raw"}
	sort.Strings(out)
	return out
}
