package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingGivesDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "en" || cfg.CaptureMode != "ptt" || cfg.WakePhrase != "log item" {
		t.Errorf("default wrong: %+v", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	cfg := Default()
	cfg.DeviceID = "dev-42"
	cfg.Language = "es"
	cfg.Sync.Endpoint = "https://example.com"
	cfg.Thresholds.Quantity = 0.9
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != "dev-42" || got.Language != "es" || got.Thresholds.Quantity != 0.9 {
		t.Errorf("round trip wrong: %+v", got)
	}
	// partial file keeps defaults for omitted keys
	if err := os.WriteFile(path, []byte(`{"device_id":"d2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != "d2" || got.WakePhrase != "log item" {
		t.Errorf("partial merge wrong: %+v", got)
	}
}

func TestValidateRejects(t *testing.T) {
	bad := []func(*Config){
		func(c *Config) { c.Language = "fr" },
		func(c *Config) { c.CaptureMode = "telepathy" },
		func(c *Config) { c.Thresholds.ASR = 1.5 },
		func(c *Config) { c.RequiredFields = []string{"vibes"} },
		func(c *Config) { c.Sync.BatchSize = -1 },
	}
	for i, mutate := range bad {
		c := Default()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("case %d should fail validation", i)
		}
	}
}

func TestLoadBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(path, []byte("{nope"), 0o600)
	if _, err := Load(path); err == nil {
		t.Error("bad JSON should fail")
	}
}
