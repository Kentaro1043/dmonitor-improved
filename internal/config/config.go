package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const RelativePath = "var/www/dmonitor.conf"

type Config struct {
	Rig         string `json:"rig"`
	LCD         string `json:"lcd"`
	Callsign    string `json:"callsign"`
	GPSSkipMode string `json:"gpsSkipMode"`
}

func Default() Config {
	return Config{
		Rig:         "ICOM",
		LCD:         "NONE",
		Callsign:    FormatCallsign(""),
		GPSSkipMode: "NO_SKIP",
	}
}

func Load(rootfs string) (Config, error) {
	path := filepath.Join(rootfs, RelativePath)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Config{}, err
	}
	defer f.Close()

	lines := make([]string, 0, 4)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	for len(lines) < 4 {
		lines = append(lines, "")
	}

	cfg := Config{
		Rig:         normalizeToken(lines[0], "ICOM"),
		LCD:         normalizeToken(lines[1], "NONE"),
		Callsign:    FormatCallsign(lines[2]),
		GPSSkipMode: normalizeGPS(lines[3]),
	}
	return cfg, nil
}

func Save(rootfs string, cfg Config) (Config, error) {
	normalized := Config{
		Rig:         normalizeToken(cfg.Rig, "ICOM"),
		LCD:         normalizeToken(cfg.LCD, "NONE"),
		Callsign:    FormatCallsign(cfg.Callsign),
		GPSSkipMode: normalizeGPS(cfg.GPSSkipMode),
	}
	path := filepath.Join(rootfs, RelativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Config{}, err
	}
	content := fmt.Sprintf("%s\n%s\n%s\n%s\n", normalized.Rig, normalized.LCD, normalized.Callsign, normalized.GPSSkipMode)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Config{}, err
	}
	return normalized, nil
}

func FormatCallsign(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) > 8 {
		value = value[:8]
	}
	return fmt.Sprintf("%-8s", value)
}

func normalizeToken(value, fallback string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func normalizeGPS(value string) string {
	value = normalizeToken(value, "NO_SKIP")
	if value == "SKIP" {
		return "SKIP"
	}
	return "NO_SKIP"
}
