package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Config contains optional configuration overrides loaded from disk.
type Config struct {
	Room       string `json:"room"`
	Brightness *int   `json:"brightness,omitempty"`
}

func loadConfig(path string) (Config, error) {
	var cfg Config
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("load config: open %q: %w", path, err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return cfg, fmt.Errorf("load config: read %q: %w", path, err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("load config: parse %q: %w", path, err)
	}

	if cfg.Brightness != nil {
		if *cfg.Brightness < 1 || *cfg.Brightness > 100 {
			return cfg, fmt.Errorf("load config: brightness must be between 1 and 100, got %d", *cfg.Brightness)
		}
	}
	return cfg, nil
}
