package config

import (
	"strings"
	"testing"
)

func TestValidateMinimumStreamBuffer(t *testing.T) {
	cfg := Defaults()
	cfg.Stations = map[string]StationConfig{
		"test": {Path: t.TempDir()},
	}
	cfg.Stream.BufferSize = minBufferSizeKB - 1

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "buffer_size") {
		t.Fatalf("Validate() error = %v, want buffer_size error", err)
	}

	cfg.Stream.BufferSize = minBufferSizeKB
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() at minimum buffer size: %v", err)
	}
}
