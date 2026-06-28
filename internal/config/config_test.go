package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFormatCallsign(t *testing.T) {
	tests := map[string]string{
		"jl1iza a":   "JL1IZA A",
		" n0call ":   "N0CALL  ",
		"too-long-x": "TOO-LONG",
		"":           "        ",
	}
	for input, want := range tests {
		if got := FormatCallsign(input); got != want {
			t.Fatalf("FormatCallsign(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLoadSave(t *testing.T) {
	root := t.TempDir()
	cfg, err := Save(root, Config{Rig: "icom", LCD: "none", Callsign: "jl1iza a", GPSSkipMode: "skip"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Callsign != "JL1IZA A" || cfg.GPSSkipMode != "SKIP" {
		t.Fatalf("unexpected normalized config: %+v", cfg)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Fatalf("Load() = %+v, want %+v", got, cfg)
	}
	raw, err := os.ReadFile(filepath.Join(root, RelativePath))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "ICOM\nNONE\nJL1IZA A\nSKIP\n" {
		t.Fatalf("unexpected raw config: %q", raw)
	}
}
