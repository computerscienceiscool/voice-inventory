package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Typos in enum flags must fail loudly, not silently no-op.
func TestSyncModeValidation(t *testing.T) {
	err := run([]string{"sync", "-db", filepath.Join(t.TempDir(), "x.db"),
		"-endpoint", "https://example.com", "-mode", "pushh"})
	if err == nil || !strings.Contains(err.Error(), "unknown -mode") {
		t.Errorf("bogus mode should error, got %v", err)
	}
}

func TestListStatusValidation(t *testing.T) {
	err := run([]string{"list", "-db", filepath.Join(t.TempDir(), "x.db"),
		"-status", "Draft"})
	if err == nil || !strings.Contains(err.Error(), "unknown -status") {
		t.Errorf("bogus status should error, got %v", err)
	}
	if err := run([]string{"list", "-db", filepath.Join(t.TempDir(), "y.db"),
		"-status", "draft"}); err != nil {
		t.Errorf("valid status should work: %v", err)
	}
}
