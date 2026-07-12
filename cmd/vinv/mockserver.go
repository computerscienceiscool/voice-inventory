package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"sync"

	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/refdata"
	"github.com/computerscienceiscool/voice-inventory/syncer"
)

// cmdMockServer runs an in-memory backend implementing the sync wire
// protocol (§10.2) for end-to-end testing: it accepts observation batches
// idempotently and serves reference data with an ETag.
func cmdMockServer(args []string) error {
	fs := flag.NewFlagSet("mockserver", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8873", "listen address (loopback by default; this dev server has no auth)")
	refFile := fs.String("refdata", "", "reference-data JSON file (RefDataResponse shape)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ref := sampleRefData()
	if *refFile != "" {
		if err := readJSON(*refFile, &ref); err != nil {
			return err
		}
	}
	srv := newMockBackend(ref)
	log.Printf("vinv mock backend on %s (POST /v1/observations:batch, GET /v1/refdata, GET /v1/records)", *addr)
	return http.ListenAndServe(*addr, srv)
}

type mockBackend struct {
	mu      sync.Mutex
	records map[string]observation.Observation
	ref     syncer.RefDataResponse
	etag    string
	mux     *http.ServeMux
}

func newMockBackend(ref syncer.RefDataResponse) *mockBackend {
	b := &mockBackend{
		records: map[string]observation.Observation{},
		ref:     ref,
		etag:    `"v1"`,
		mux:     http.NewServeMux(),
	}
	b.mux.HandleFunc("POST /v1/observations:batch", b.handleBatch)
	b.mux.HandleFunc("POST /v1/observations:void", b.handleVoid)
	b.mux.HandleFunc("GET /v1/refdata", b.handleRefData)
	b.mux.HandleFunc("GET /v1/records", b.handleRecords)
	return b
}

// handleVoid tombstones records the device discarded after upload
// (docs/backend-protocol.md, TODO item 112).
func (b *mockBackend) handleVoid(w http.ResponseWriter, r *http.Request) {
	var req syncer.VoidRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var resp syncer.VoidResponse
	b.mu.Lock()
	for _, id := range req.IDs {
		delete(b.records, id) // idempotent: unknown ids ack too
		resp.Voided = append(resp.Voided, id)
	}
	b.mu.Unlock()
	log.Printf("void from %s: %d record(s)", req.DeviceID, len(resp.Voided))
	writeJSON(w, resp)
}

func (b *mockBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) { b.mux.ServeHTTP(w, r) }

func (b *mockBackend) handleBatch(w http.ResponseWriter, r *http.Request) {
	var req syncer.PushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var resp syncer.PushResponse
	b.mu.Lock()
	for _, o := range req.Observations {
		if err := o.Validate(); err != nil {
			resp.Rejected = append(resp.Rejected, syncer.RejectedRecord{
				ID: o.ID, Reason: err.Error(),
			})
			continue
		}
		// idempotent by id: re-uploads overwrite the same record
		b.records[o.ID] = o
		resp.Accepted = append(resp.Accepted, o.ID)
	}
	total := len(b.records)
	b.mu.Unlock()
	log.Printf("batch from %s: %d accepted, %d rejected (%d total)",
		req.DeviceID, len(resp.Accepted), len(resp.Rejected), total)
	writeJSON(w, resp)
}

func (b *mockBackend) handleRefData(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	etag := b.etag
	ref := b.ref
	b.mu.Unlock()
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	writeJSON(w, ref)
}

func (b *mockBackend) handleRecords(w http.ResponseWriter, _ *http.Request) {
	b.mu.Lock()
	out := make([]observation.Observation, 0, len(b.records))
	for _, o := range b.records {
		out = append(out, o)
	}
	b.mu.Unlock()
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func sampleRefData() syncer.RefDataResponse {
	return syncer.RefDataResponse{
		Locations: []refdata.Location{
			{ID: "LOC-A14", Name: "Bin A-14", Aliases: []string{"A-14", "A fourteen"}},
			{ID: "LOC-C7", Name: "Bin C-7", Aliases: []string{"C-7", "C seven"}},
			{ID: "LOC-AISLE5-S2", Name: "Aisle 5 Shelf 2", Aliases: []string{"aisle 5 shelf 2"}},
		},
		Parts: []refdata.Part{
			{PartNumber: "PN-1001", Name: "RJ45 connector", Aliases: []string{"RJ45 connectors", "ethernet connectors"}},
			{PartNumber: "PN-2002", Name: "Cat6 cable", Aliases: []string{"Cat6", "category six"}},
		},
		Units: []refdata.Unit{
			{Name: "skid", Language: "en", Aliases: []string{"skids"}},
			{Name: "tarima", Language: "es", Aliases: []string{"tarimas"}},
		},
	}
}
