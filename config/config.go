// Package config holds the per-device profile (spec §14): model choice,
// capture mode, language, retention policy, sync endpoint, and confidence
// thresholds. Admins manage these values; the app loads them at start.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Model selects and locates Whisper weights (spec §8.2).
type Model struct {
	Name    string `json:"name"`    // e.g. "ggml-small-q5_1.bin"
	Path    string `json:"path"`    // explicit weights path; wins over URL fetch
	URL     string `json:"url"`     // fetch-once source when not bundled
	SHA256  string `json:"sha256"`  // integrity check for fetched weights
	Threads int    `json:"threads"` // 0 = engine default
}

// Retention is the audio-clip retention policy (spec §6.3).
type Retention struct {
	Enabled  bool `json:"enabled"`
	KeepDays int  `json:"keep_days"` // purge after sync + N days
}

// Sync configures the backend endpoint (spec §10.2, §11 Phase A).
type Sync struct {
	Endpoint      string `json:"endpoint"` // e.g. https://inventory.example.com
	Token         string `json:"token"`    // bearer credential
	BatchSize     int    `json:"batch_size"`
	AllowInsecure bool   `json:"allow_insecure"` // permit http:// (dev only)
}

// Thresholds are per-field confidence levels below which a field is
// flagged doubtful and confirmation is forced (spec §13, §14).
type Thresholds struct {
	ASR      float64 `json:"asr"`
	Quantity float64 `json:"quantity"`
	Location float64 `json:"location"`
	Item     float64 `json:"item"`
}

// Config is the full device profile.
type Config struct {
	DeviceID    string `json:"device_id"`
	OperatorID  string `json:"operator_id"`
	Language    string `json:"language"`     // en | es | auto
	CaptureMode string `json:"capture_mode"` // ptt | wake
	WakePhrase  string `json:"wake_phrase"`

	Model     Model     `json:"model"`
	Retention Retention `json:"retention"`
	Sync      Sync      `json:"sync"`

	Thresholds Thresholds `json:"thresholds"`
	// AutoConfirmHighConfidence saves without explicit confirmation when
	// every field clears its threshold. Default false: §4.1 always confirms
	// (spec ambiguity resolved conservatively — TODO item 062).
	AutoConfirmHighConfidence bool `json:"auto_confirm_high_confidence"`
	// RequiredFields lists fields that flag a record for review when empty.
	// Capture is never blocked (§13); default: item.
	RequiredFields []string `json:"required_fields"`
	// MultiItem enables "…and…" utterance splitting (P4, default off).
	MultiItem bool `json:"multi_item"`
	// TargetLatencyMS is the §8.4 utterance-end → readback target used by
	// latency instrumentation to suggest a smaller model.
	TargetLatencyMS int `json:"target_latency_ms"`
	// HighPassHz cuts low-frequency warehouse rumble before VAD/ASR
	// (§8.3 step 3). 0 disables the filter.
	HighPassHz int `json:"high_pass_hz"`
}

// Default returns the shipped configuration.
func Default() Config {
	return Config{
		Language:    "en",
		CaptureMode: "ptt",
		WakePhrase:  "log item",
		Model: Model{
			Name: "ggml-small-q5_1.bin",
		},
		Retention: Retention{Enabled: true, KeepDays: 7},
		Sync:      Sync{BatchSize: 50},
		Thresholds: Thresholds{
			ASR: 0.70, Quantity: 0.70, Location: 0.70, Item: 0.60,
		},
		RequiredFields:  []string{"item"},
		TargetLatencyMS: 3000,
		HighPassHz:      100,
	}
}

// Load reads a config file; a missing file yields Default().
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes the config as indented JSON, creating parent directories.
func (c Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Validate checks value ranges and enumerations.
func (c Config) Validate() error {
	switch c.Language {
	case "en", "es", "auto":
	default:
		return fmt.Errorf("config: unsupported language %q", c.Language)
	}
	switch c.CaptureMode {
	case "ptt", "wake":
	default:
		return fmt.Errorf("config: unsupported capture_mode %q", c.CaptureMode)
	}
	if c.Retention.Enabled && c.Retention.KeepDays < 0 {
		return errors.New("config: retention.keep_days must be ≥ 0")
	}
	if c.Sync.BatchSize < 0 {
		return errors.New("config: sync.batch_size must be ≥ 0")
	}
	if c.HighPassHz < 0 {
		return errors.New("config: high_pass_hz must be ≥ 0")
	}
	for _, v := range []float64{c.Thresholds.ASR, c.Thresholds.Quantity,
		c.Thresholds.Location, c.Thresholds.Item} {
		if v < 0 || v > 1 {
			return fmt.Errorf("config: threshold %v out of [0,1]", v)
		}
	}
	for _, f := range c.RequiredFields {
		switch f {
		case "item", "quantity", "location", "unit", "description":
		default:
			return fmt.Errorf("config: unknown required field %q", f)
		}
	}
	return nil
}
