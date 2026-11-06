package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shubhendu-abhishek/gpu-telemetry-pipeline/internal/api"
)

func main() {
	output := flag.String("output", "api/openapi.json", "output path for generated OpenAPI JSON")
	flag.Parse()
	if err := generate(*output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func generate(output string) error {
	data, err := json.MarshalIndent(api.OpenAPISpec(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(output, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("generated %s\n", output)
	return nil
}
