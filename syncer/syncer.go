// Package syncer implements offline-first sync (spec §10.2): opportunistic,
// resumable, one-way-up for observations and one-way-down for reference
// data, idempotent by UUIDv7 record id. The Syncer interface lets the
// transport swap from plain HTTPS (Phase A) to a PromiseGrid agent
// (Phase B) without touching capture code.
package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/refdata"
	"github.com/computerscienceiscool/voice-inventory/store"
)

// PushReport summarizes one push pass.
type PushReport struct {
	Pushed   int              `json:"pushed"`
	Batches  int              `json:"batches"`
	Rejected []RejectedRecord `json:"rejected,omitempty"`
}

// RejectedRecord is a record the backend refused (kept confirmed locally
// for supervisor attention; it will be retried on later pushes).
type RejectedRecord struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// PullReport summarizes a reference-data pull.
type PullReport struct {
	Locations   int  `json:"locations"`
	Parts       int  `json:"parts"`
	Units       int  `json:"units"`
	NotModified bool `json:"not_modified"`
}

// Syncer is the transport-independent sync contract (spec §10.2).
type Syncer interface {
	// Push uploads confirmed records in batches, marking accepted ones
	// synced. Safe to call repeatedly; retries are idempotent by id.
	Push(ctx context.Context) (PushReport, error)
	// PullRefData refreshes the cached locations/parts/units.
	PullRefData(ctx context.Context) (PullReport, error)
}

// Wire types shared with the backend (and the vinv mock server).
type (
	// PushRequest is the body of POST /v1/observations:batch.
	PushRequest struct {
		DeviceID     string                    `json:"device_id"`
		Observations []observation.Observation `json:"observations"`
	}
	// PushResponse acknowledges a batch.
	PushResponse struct {
		Accepted []string         `json:"accepted"`
		Rejected []RejectedRecord `json:"rejected,omitempty"`
	}
	// RefDataResponse is the body of GET /v1/refdata.
	RefDataResponse struct {
		Locations []refdata.Location `json:"locations"`
		Parts     []refdata.Part     `json:"parts"`
		Units     []refdata.Unit     `json:"units"`
	}
)

const (
	etagKey     = "refdata_etag"
	lastPushKey = "last_push_at"
	lastPullKey = "last_pull_at"

	// maxResponseBytes bounds any single backend response (64 MB covers a
	// very large reference-data set).
	maxResponseBytes = 64 << 20
)

// Options configures the HTTPS syncer.
type Options struct {
	BaseURL   string // e.g. https://inventory.example.com
	Token     string // bearer credential
	DeviceID  string
	BatchSize int // default 50
	// AllowInsecure permits http:// endpoints (development only).
	AllowInsecure bool
	// MaxAttempts per request on transient failures (default 3).
	MaxAttempts int
	// Client overrides the HTTP client (default: 30 s timeout).
	Client *http.Client
	// Backoff returns the wait before retry attempt n (1-based); test hook.
	Backoff func(attempt int) time.Duration
}

// HTTP is the Phase-A Syncer over HTTPS.
type HTTP struct {
	st   *store.Store
	opts Options
}

// ErrInsecureEndpoint rejects plaintext endpoints unless explicitly allowed.
var ErrInsecureEndpoint = errors.New("syncer: endpoint is not https (set AllowInsecure for development)")

// NewHTTP validates the endpoint and builds an HTTPS syncer.
func NewHTTP(st *store.Store, opts Options) (*HTTP, error) {
	if st == nil {
		return nil, errors.New("syncer: store is required")
	}
	u, err := url.Parse(opts.BaseURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("syncer: invalid base URL %q", opts.BaseURL)
	}
	if u.Scheme != "https" && !opts.AllowInsecure {
		return nil, ErrInsecureEndpoint
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 50
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.Backoff == nil {
		opts.Backoff = func(attempt int) time.Duration {
			return time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
		}
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	return &HTTP{st: st, opts: opts}, nil
}

// Push implements Syncer.
func (h *HTTP) Push(ctx context.Context) (PushReport, error) {
	var report PushReport
	// Cursor pagination: every confirmed record is offered exactly once per
	// pass. Backend-rejected records stay confirmed and are retried on the
	// next pass without blocking the records queued behind them.
	cursor := ""
	for {
		batch, err := h.st.UnsyncedConfirmed(cursor, h.opts.BatchSize)
		if err != nil {
			return report, err
		}
		if len(batch) == 0 {
			break
		}
		cursor = batch[len(batch)-1].ID

		req := PushRequest{DeviceID: h.opts.DeviceID}
		for _, o := range batch {
			req.Observations = append(req.Observations, *o)
		}
		var resp PushResponse
		if err := h.do(ctx, http.MethodPost, "/v1/observations:batch", req, nil, &resp, nil); err != nil {
			return report, err
		}
		n, err := h.st.MarkSynced(resp.Accepted, time.Now())
		if err != nil {
			return report, fmt.Errorf("syncer: mark synced: %w", err)
		}
		report.Pushed += n
		report.Batches++
		report.Rejected = append(report.Rejected, resp.Rejected...)
		if len(batch) < h.opts.BatchSize {
			break
		}
	}
	if report.Batches > 0 {
		_ = h.st.SetSyncState(lastPushKey, time.Now().UTC().Format(time.RFC3339))
	}
	return report, nil
}

// PullRefData implements Syncer with ETag-based caching.
func (h *HTTP) PullRefData(ctx context.Context) (PullReport, error) {
	var report PullReport
	etag, err := h.st.GetSyncState(etagKey)
	if err != nil {
		return report, err
	}
	headers := map[string]string{}
	if etag != "" {
		headers["If-None-Match"] = etag
	}
	var resp RefDataResponse
	var newTag string
	err = h.do(ctx, http.MethodGet, "/v1/refdata", nil, headers, &resp, func(hr *http.Response) {
		newTag = hr.Header.Get("ETag")
	})
	if errors.Is(err, errNotModified) {
		report.NotModified = true
		return report, nil
	}
	if err != nil {
		return report, err
	}
	if err := h.st.ReplaceLocations(resp.Locations); err != nil {
		return report, err
	}
	if err := h.st.ReplaceParts(resp.Parts); err != nil {
		return report, err
	}
	if err := h.st.ReplaceUnits(resp.Units); err != nil {
		return report, err
	}
	report.Locations = len(resp.Locations)
	report.Parts = len(resp.Parts)
	report.Units = len(resp.Units)
	if newTag != "" {
		if err := h.st.SetSyncState(etagKey, newTag); err != nil {
			return report, err
		}
	}
	_ = h.st.SetSyncState(lastPullKey, time.Now().UTC().Format(time.RFC3339))
	return report, nil
}

var errNotModified = errors.New("not modified")

// do performs one request with bounded retries on transient failures.
func (h *HTTP) do(ctx context.Context, method, path string, body any,
	headers map[string]string, out any, onResp func(*http.Response)) error {

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	var lastErr error
	for attempt := 1; attempt <= h.opts.MaxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(h.opts.Backoff(attempt - 1)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, h.opts.BaseURL+path,
			bytes.NewReader(payload))
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if h.opts.Token != "" {
			req.Header.Set("Authorization", "Bearer "+h.opts.Token)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := h.opts.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("syncer: %s %s: %w", method, path, err)
			continue // network error → retry
		}
		func() {
			defer resp.Body.Close()
			switch {
			case resp.StatusCode == http.StatusNotModified:
				lastErr = errNotModified
			case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				lastErr = fmt.Errorf("syncer: %s %s: HTTP %d: %s",
					method, path, resp.StatusCode, strings.TrimSpace(string(b)))
			case resp.StatusCode >= 400:
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				lastErr = &FatalError{Status: resp.StatusCode,
					Message: strings.TrimSpace(string(b))}
			default:
				if onResp != nil {
					onResp(resp)
				}
				if out != nil {
					// Bound the response so a compromised or broken server
					// can't exhaust device memory.
					dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
					if err := dec.Decode(out); err != nil {
						lastErr = fmt.Errorf("syncer: decode response: %w", err)
						return
					}
				}
				lastErr = nil
			}
		}()
		if lastErr == nil || errors.Is(lastErr, errNotModified) {
			return lastErr
		}
		var fatal *FatalError
		if errors.As(lastErr, &fatal) {
			return lastErr // 4xx: retrying won't help
		}
	}
	return lastErr
}

// FatalError is a non-retryable HTTP failure (auth, bad request).
type FatalError struct {
	Status  int
	Message string
}

func (e *FatalError) Error() string {
	return fmt.Sprintf("syncer: HTTP %d: %s", e.Status, e.Message)
}
