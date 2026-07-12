package asr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ModelSpec describes a Whisper weights file to fetch once and cache
// (spec §8.2: weights are bundled or fetched on first run, never per use).
type ModelSpec struct {
	Name   string // file name in the cache dir, e.g. "ggml-small-q5_1.bin"
	URL    string // download source
	SHA256 string // hex digest; required for downloads unless AllowUnverified
	// AllowUnverified permits fetching without a checksum (development
	// only): the model parser is C++ code fed by this file, so integrity
	// matters.
	AllowUnverified bool
}

// ModelProgress reports download progress; total is -1 when unknown.
type ModelProgress func(fetchedBytes, totalBytes int64)

// EnsureModel returns the local path of the cached weights, downloading and
// verifying them on first use. A corrupt cached file is deleted and
// re-fetched (spec §13 "model missing/corrupt"). The download goes to a
// temporary file and is renamed only after the checksum passes, so a killed
// download never leaves a half-written model in place.
func EnsureModel(ctx context.Context, cacheDir string, spec ModelSpec, progress ModelProgress) (string, error) {
	if spec.Name == "" {
		return "", errors.New("asr: model spec needs a name")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(cacheDir, spec.Name)

	if ok, err := verifyFile(path, spec.SHA256); err == nil && ok {
		return path, nil
	} else if err == nil && !ok {
		// present but corrupt → refetch
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			return "", fmt.Errorf("asr: remove corrupt model: %w", rmErr)
		}
	}

	if spec.URL == "" {
		return "", fmt.Errorf("asr: model %q not cached and no URL configured", spec.Name)
	}
	if spec.SHA256 == "" && !spec.AllowUnverified {
		return "", fmt.Errorf("asr: refusing to download model %q without a sha256 (set AllowUnverified for development)", spec.Name)
	}
	if err := download(ctx, spec, path, progress); err != nil {
		return "", err
	}
	if ok, err := verifyFile(path, spec.SHA256); err != nil {
		return "", err
	} else if !ok {
		_ = os.Remove(path)
		return "", fmt.Errorf("asr: downloaded model %q failed checksum", spec.Name)
	}
	return path, nil
}

// verifyFile returns (true, nil) when the file exists and matches the
// digest (or when no digest is configured), (false, nil) when it exists but
// mismatches, and an error only for I/O problems. A missing file is
// (false, nil).
func verifyFile(path, sha string) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	if sha == "" {
		return true, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sha), nil
}

func download(ctx context.Context, spec ModelSpec, dest string, progress ModelProgress) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("asr: fetch model: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("asr: fetch model: HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".model-*.part")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op after successful rename
	}()

	total := resp.ContentLength
	var fetched int64
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				return werr
			}
			fetched += int64(n)
			if progress != nil {
				progress(fetched, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("asr: fetch model: %w", rerr)
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}
