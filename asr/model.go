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
	"sync"
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

// modelLocks serializes EnsureModel per cache path so concurrent callers
// fetch once and can never clobber each other's verified install.
var (
	modelLocksMu sync.Mutex
	modelLocks   = map[string]*sync.Mutex{}
)

func modelLock(path string) *sync.Mutex {
	modelLocksMu.Lock()
	defer modelLocksMu.Unlock()
	if l, ok := modelLocks[path]; ok {
		return l
	}
	l := &sync.Mutex{}
	modelLocks[path] = l
	return l
}

// EnsureModel returns the local path of the cached weights, downloading and
// verifying them on first use. A corrupt cached file is deleted and
// re-fetched (spec §13 "model missing/corrupt"). The download goes to a
// temporary file which is verified BEFORE being renamed into place, so a
// killed or corrupt download never replaces a good model; concurrent calls
// for the same model serialize and share one download.
func EnsureModel(ctx context.Context, cacheDir string, spec ModelSpec, progress ModelProgress) (string, error) {
	if spec.Name == "" {
		return "", errors.New("asr: model spec needs a name")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(cacheDir, spec.Name)

	l := modelLock(path)
	l.Lock()
	defer l.Unlock()

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
	tmpName, err := download(ctx, spec, path, progress)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpName) // no-op after a successful rename
	if ok, err := verifyFile(tmpName, spec.SHA256); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("asr: downloaded model %q failed checksum", spec.Name)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
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

// download fetches the weights into a temporary file next to dest and
// returns its name; the caller verifies it before renaming it into place.
func download(ctx context.Context, spec ModelSpec, dest string, progress ModelProgress) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("asr: fetch model: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asr: fetch model: HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".model-*.part")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	success := false
	defer func() {
		_ = tmp.Close()
		if !success {
			_ = os.Remove(tmpName)
		}
	}()

	total := resp.ContentLength
	var fetched int64
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				return "", werr
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
			return "", fmt.Errorf("asr: fetch model: %w", rerr)
		}
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	success = true
	return tmpName, nil
}
