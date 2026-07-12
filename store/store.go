// Package store is the on-device SQLite persistence layer (spec §10.1): the
// observation queue, cached reference data, and sync state. All operations
// are local and never touch the network. WAL journaling plus synchronous
// FULL means confirmed records survive a crash or force-quit (§12).
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver

	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/refdata"
)

// ErrNotFound is returned when a record id does not exist.
var ErrNotFound = errors.New("store: not found")

// ErrImmutable is returned when writing to a synced or rejected record.
var ErrImmutable = errors.New("store: record is synced or rejected and cannot change")

// ErrBadTransition is returned for an illegal status change.
var ErrBadTransition = errors.New("store: illegal status transition")

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and migrates it.
func Open(path string) (*Store, error) {
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(10000)" +
		"&_pragma=synchronous(FULL)"
	return open(dsn)
}

// OpenMemory opens an in-memory database (tests, previews).
func OpenMemory() (*Store, error) {
	return open("file::memory:?_pragma=foreign_keys(1)")
}

func open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite has a single writer; one connection avoids lock contention and
	// keeps in-memory databases alive.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

var migrations = []string{
	`
CREATE TABLE observations (
	id             TEXT PRIMARY KEY,
	device_id      TEXT NOT NULL DEFAULT '',
	operator_id    TEXT NOT NULL DEFAULT '',
	captured_at    TEXT NOT NULL,
	language       TEXT NOT NULL DEFAULT 'en',
	raw_transcript TEXT NOT NULL DEFAULT '',
	audio_ref      TEXT,
	item_text      TEXT NOT NULL DEFAULT '',
	part_number    TEXT,
	quantity       REAL,
	unit           TEXT,
	location_text  TEXT NOT NULL DEFAULT '',
	location_id    TEXT,
	description    TEXT,
	conf_asr       REAL NOT NULL DEFAULT 0,
	conf_quantity  REAL NOT NULL DEFAULT 0,
	conf_location  REAL NOT NULL DEFAULT 0,
	conf_item      REAL NOT NULL DEFAULT 0,
	status         TEXT NOT NULL CHECK (status IN ('draft','confirmed','synced','rejected')),
	needs_review   INTEGER NOT NULL DEFAULT 0,
	review_reasons TEXT NOT NULL DEFAULT '[]',
	schema_version INTEGER NOT NULL DEFAULT 1,
	created_at     TEXT NOT NULL,
	updated_at     TEXT NOT NULL,
	synced_at      TEXT
);
CREATE INDEX idx_obs_status ON observations(status, id);
CREATE INDEX idx_obs_captured ON observations(captured_at);
CREATE TABLE corrections (
	seq            INTEGER PRIMARY KEY AUTOINCREMENT,
	observation_id TEXT NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
	field          TEXT NOT NULL,
	from_value     TEXT NOT NULL,
	to_value       TEXT NOT NULL,
	at             TEXT NOT NULL
);
CREATE INDEX idx_corr_obs ON corrections(observation_id, seq);
CREATE TABLE ref_locations (
	id      TEXT PRIMARY KEY,
	name    TEXT NOT NULL DEFAULT '',
	aliases TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE ref_parts (
	part_number TEXT PRIMARY KEY,
	name        TEXT NOT NULL DEFAULT '',
	aliases     TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE ref_units (
	name     TEXT NOT NULL,
	language TEXT NOT NULL DEFAULT '',
	aliases  TEXT NOT NULL DEFAULT '[]',
	PRIMARY KEY (name, language)
);
CREATE TABLE sync_state (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`,
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("set user_version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Observations

// timeFormat is RFC3339 with FIXED-WIDTH nanoseconds. time.RFC3339Nano
// trims trailing zeros, which breaks the lexicographic-equals-chronological
// property that SQL string comparisons (AudioToPurge's synced_at < cutoff)
// and ordering rely on: "…00.5Z" would sort before "…00Z".
const timeFormat = "2006-01-02T15:04:05.000000000Z07:00"

func fmtTime(t time.Time) string { return t.UTC().Format(timeFormat) }

// parseTime accepts the fixed-width form and any RFC3339Nano variant.
func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

func nullStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// Insert writes a new observation (normally a draft). The record must
// validate first.
func (s *Store) Insert(o *observation.Observation) error {
	if err := o.Validate(); err != nil {
		return fmt.Errorf("store insert: %w", err)
	}
	reasons, err := json.Marshal(o.ReviewReasons)
	if err != nil {
		return err
	}
	now := fmtTime(time.Now())
	_, err = s.db.Exec(`
		INSERT INTO observations (
			id, device_id, operator_id, captured_at, language, raw_transcript,
			audio_ref, item_text, part_number, quantity, unit, location_text,
			location_id, description, conf_asr, conf_quantity, conf_location,
			conf_item, status, needs_review, review_reasons, schema_version,
			created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.DeviceID, o.OperatorID, fmtTime(o.CapturedAt), o.Language,
		o.RawTranscript, nullStr(o.AudioRef), o.Parsed.ItemText,
		nullStr(o.Parsed.PartNumber), nullFloat(o.Parsed.Quantity),
		nullStr(o.Parsed.Unit), o.Parsed.LocationText,
		nullStr(o.Parsed.LocationID), nullStr(o.Parsed.Description),
		o.Confidence.ASR, o.Confidence.Quantity, o.Confidence.Location,
		o.Confidence.Item, string(o.Status), boolInt(o.NeedsReview),
		string(reasons), o.SchemaVersion, now, now,
	)
	if err != nil {
		return fmt.Errorf("store insert: %w", err)
	}
	for _, c := range o.Corrections {
		if err := s.AddCorrection(o.ID, c); err != nil {
			return err
		}
	}
	return nil
}

// Update rewrites the mutable fields of a draft or confirmed record.
// Synced and rejected records are immutable (§10.2: the backend owns synced
// records; edits after sync are rejected — spec decision, TODO item 067).
func (s *Store) Update(o *observation.Observation) error {
	if err := o.Validate(); err != nil {
		return fmt.Errorf("store update: %w", err)
	}
	reasons, err := json.Marshal(o.ReviewReasons)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`
		UPDATE observations SET
			language = ?, raw_transcript = ?, audio_ref = ?, item_text = ?,
			part_number = ?, quantity = ?, unit = ?, location_text = ?,
			location_id = ?, description = ?, conf_asr = ?, conf_quantity = ?,
			conf_location = ?, conf_item = ?, needs_review = ?,
			review_reasons = ?, updated_at = ?
		WHERE id = ? AND status IN ('draft','confirmed')`,
		o.Language, o.RawTranscript, nullStr(o.AudioRef), o.Parsed.ItemText,
		nullStr(o.Parsed.PartNumber), nullFloat(o.Parsed.Quantity),
		nullStr(o.Parsed.Unit), o.Parsed.LocationText,
		nullStr(o.Parsed.LocationID), nullStr(o.Parsed.Description),
		o.Confidence.ASR, o.Confidence.Quantity, o.Confidence.Location,
		o.Confidence.Item, boolInt(o.NeedsReview), string(reasons),
		fmtTime(time.Now()), o.ID,
	)
	if err != nil {
		return fmt.Errorf("store update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.Get(o.ID); errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return ErrImmutable
	}
	return nil
}

// AddCorrection appends one field-change record to the corrections log.
func (s *Store) AddCorrection(obsID string, c observation.Correction) error {
	_, err := s.db.Exec(`
		INSERT INTO corrections (observation_id, field, from_value, to_value, at)
		VALUES (?,?,?,?,?)`,
		obsID, c.Field, c.From, c.To, fmtTime(c.At))
	if err != nil {
		return fmt.Errorf("store add correction: %w", err)
	}
	return nil
}

// SetStatus performs a validated lifecycle transition.
func (s *Store) SetStatus(id string, to observation.Status) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var cur string
	err = tx.QueryRow(`SELECT status FROM observations WHERE id = ?`, id).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !observation.CanTransition(observation.Status(cur), to) {
		return fmt.Errorf("%w: %s → %s", ErrBadTransition, cur, to)
	}
	now := fmtTime(time.Now())
	if to == observation.StatusSynced {
		_, err = tx.Exec(`UPDATE observations SET status = ?, synced_at = ?, updated_at = ? WHERE id = ?`,
			string(to), now, now, id)
	} else {
		_, err = tx.Exec(`UPDATE observations SET status = ?, updated_at = ? WHERE id = ?`,
			string(to), now, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Confirm moves a draft to confirmed.
func (s *Store) Confirm(id string) error { return s.SetStatus(id, observation.StatusConfirmed) }

// Reject marks a draft or confirmed record rejected ("scratch that", §13).
func (s *Store) Reject(id string) error { return s.SetStatus(id, observation.StatusRejected) }

// Get loads one observation with its corrections.
func (s *Store) Get(id string) (*observation.Observation, error) {
	rows, err := s.query(`WHERE o.id = ?`, "", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0], nil
}

// Filter selects observations for List.
type Filter struct {
	Status      observation.Status // empty = all
	NeedsReview *bool
	Limit       int // 0 = no limit
	Offset      int
}

// List returns observations newest-first (UUIDv7 ids are time-ordered).
func (s *Store) List(f Filter) ([]*observation.Observation, error) {
	var conds []string
	var args []any
	if f.Status != "" {
		conds = append(conds, "o.status = ?")
		args = append(args, string(f.Status))
	}
	if f.NeedsReview != nil {
		conds = append(conds, "o.needs_review = ?")
		args = append(args, boolInt(*f.NeedsReview))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	tail := "ORDER BY o.id DESC"
	switch {
	case f.Limit > 0:
		tail += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, f.Offset)
	case f.Offset > 0:
		// SQLite requires a LIMIT clause for OFFSET; -1 means unlimited.
		tail += fmt.Sprintf(" LIMIT -1 OFFSET %d", f.Offset)
	}
	return s.query(where, tail, args...)
}

// UnsyncedConfirmed returns confirmed records oldest-first for upload,
// starting strictly after afterID ("" = from the beginning). The cursor
// lets a push pass walk the whole queue even when the backend rejects some
// records (they stay confirmed and are retried on the next pass) — without
// it, rejected records at the head would starve everything behind them.
func (s *Store) UnsyncedConfirmed(afterID string, limit int) ([]*observation.Observation, error) {
	tail := "ORDER BY o.id ASC"
	if limit > 0 {
		tail += fmt.Sprintf(" LIMIT %d", limit)
	}
	return s.query(`WHERE o.status = 'confirmed' AND o.id > ?`, tail, afterID)
}

// MarkSynced transitions the given confirmed records to synced in one
// transaction and returns how many actually changed.
func (s *Store) MarkSynced(ids []string, at time.Time) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stamp := fmtTime(at)
	now := fmtTime(time.Now())
	total := 0
	for _, id := range ids {
		res, err := tx.Exec(`
			UPDATE observations SET status = 'synced', synced_at = ?, updated_at = ?
			WHERE id = ? AND status = 'confirmed'`, stamp, now, id)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		total += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}

// LastActive returns the most recent draft or confirmed record's id —
// the target of "scratch that" when nothing is pending.
func (s *Store) LastActive() (string, error) {
	var id string
	err := s.db.QueryRow(`
		SELECT id FROM observations
		WHERE status IN ('draft','confirmed')
		ORDER BY id DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// CountsByStatus returns record counts per status for review badges.
func (s *Store) CountsByStatus() (map[observation.Status]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM observations GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[observation.Status]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[observation.Status(st)] = n
	}
	return out, rows.Err()
}

// AudioPurgeCandidate is a synced record still holding an audio clip.
type AudioPurgeCandidate struct {
	ID       string
	AudioRef string
}

// AudioToPurge lists synced records older than the cutoff that still have
// audio (retention policy, §6.3).
func (s *Store) AudioToPurge(syncedBefore time.Time) ([]AudioPurgeCandidate, error) {
	rows, err := s.db.Query(`
		SELECT id, audio_ref FROM observations
		WHERE status = 'synced' AND audio_ref IS NOT NULL AND synced_at < ?`,
		fmtTime(syncedBefore))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AudioPurgeCandidate
	for rows.Next() {
		var c AudioPurgeCandidate
		if err := rows.Scan(&c.ID, &c.AudioRef); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClearAudioRef removes the audio reference after the clip file is deleted.
// Synced records stay otherwise immutable; this is the one sanctioned change.
func (s *Store) ClearAudioRef(id string) error {
	_, err := s.db.Exec(`UPDATE observations SET audio_ref = NULL, updated_at = ? WHERE id = ?`,
		fmtTime(time.Now()), id)
	return err
}

func (s *Store) query(where, tail string, args ...any) ([]*observation.Observation, error) {
	q := `
		SELECT o.id, o.device_id, o.operator_id, o.captured_at, o.language,
			o.raw_transcript, o.audio_ref, o.item_text, o.part_number,
			o.quantity, o.unit, o.location_text, o.location_id, o.description,
			o.conf_asr, o.conf_quantity, o.conf_location, o.conf_item,
			o.status, o.needs_review, o.review_reasons, o.schema_version
		FROM observations o ` + where + " " + tail
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store query: %w", err)
	}
	defer rows.Close()
	var out []*observation.Observation
	for rows.Next() {
		o := &observation.Observation{}
		var capturedAt, reasons, status string
		var audioRef, partNumber, unit, locationID, description sql.NullString
		var quantity sql.NullFloat64
		var needsReview int
		if err := rows.Scan(&o.ID, &o.DeviceID, &o.OperatorID, &capturedAt,
			&o.Language, &o.RawTranscript, &audioRef, &o.Parsed.ItemText,
			&partNumber, &quantity, &unit, &o.Parsed.LocationText,
			&locationID, &description, &o.Confidence.ASR,
			&o.Confidence.Quantity, &o.Confidence.Location,
			&o.Confidence.Item, &status, &needsReview, &reasons,
			&o.SchemaVersion); err != nil {
			return nil, err
		}
		t, err := parseTime(capturedAt)
		if err != nil {
			return nil, fmt.Errorf("bad captured_at for %s: %w", o.ID, err)
		}
		o.CapturedAt = t
		o.Status = observation.Status(status)
		o.NeedsReview = needsReview != 0
		if err := json.Unmarshal([]byte(reasons), &o.ReviewReasons); err != nil {
			return nil, fmt.Errorf("bad review_reasons for %s: %w", o.ID, err)
		}
		o.AudioRef = strPtr(audioRef)
		o.Parsed.PartNumber = strPtr(partNumber)
		o.Parsed.Unit = strPtr(unit)
		o.Parsed.LocationID = strPtr(locationID)
		o.Parsed.Description = strPtr(description)
		if quantity.Valid {
			v := quantity.Float64
			o.Parsed.Quantity = &v
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, o := range out {
		if err := s.loadCorrections(o); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) loadCorrections(o *observation.Observation) error {
	rows, err := s.db.Query(`
		SELECT field, from_value, to_value, at FROM corrections
		WHERE observation_id = ? ORDER BY seq`, o.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c observation.Correction
		var at string
		if err := rows.Scan(&c.Field, &c.From, &c.To, &at); err != nil {
			return err
		}
		t, err := parseTime(at)
		if err != nil {
			return fmt.Errorf("bad correction time for %s: %w", o.ID, err)
		}
		c.At = t
		o.Corrections = append(o.Corrections, c)
	}
	return rows.Err()
}

func strPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Reference data (synced down, §6.2)

// ReplaceLocations swaps the cached locations list atomically.
func (s *Store) ReplaceLocations(locs []refdata.Location) error {
	return s.replaceRef(`DELETE FROM ref_locations`,
		`INSERT INTO ref_locations (id, name, aliases) VALUES (?,?,?)`,
		len(locs), func(i int) ([]any, error) {
			a, err := json.Marshal(locs[i].Aliases)
			if err != nil {
				return nil, err
			}
			return []any{locs[i].ID, locs[i].Name, string(a)}, nil
		})
}

// ReplaceParts swaps the cached part vocabulary atomically.
func (s *Store) ReplaceParts(parts []refdata.Part) error {
	return s.replaceRef(`DELETE FROM ref_parts`,
		`INSERT INTO ref_parts (part_number, name, aliases) VALUES (?,?,?)`,
		len(parts), func(i int) ([]any, error) {
			a, err := json.Marshal(parts[i].Aliases)
			if err != nil {
				return nil, err
			}
			return []any{parts[i].PartNumber, parts[i].Name, string(a)}, nil
		})
}

// ReplaceUnits swaps the cached unit vocabulary atomically.
func (s *Store) ReplaceUnits(units []refdata.Unit) error {
	return s.replaceRef(`DELETE FROM ref_units`,
		`INSERT INTO ref_units (name, language, aliases) VALUES (?,?,?)`,
		len(units), func(i int) ([]any, error) {
			a, err := json.Marshal(units[i].Aliases)
			if err != nil {
				return nil, err
			}
			return []any{units[i].Name, units[i].Language, string(a)}, nil
		})
}

func (s *Store) replaceRef(deleteSQL, insertSQL string, n int, argFn func(int) ([]any, error)) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(deleteSQL); err != nil {
		return err
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i := 0; i < n; i++ {
		args, err := argFn(i)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Locations returns the cached locations list.
func (s *Store) Locations() ([]refdata.Location, error) {
	rows, err := s.db.Query(`SELECT id, name, aliases FROM ref_locations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []refdata.Location
	for rows.Next() {
		var l refdata.Location
		var aliases string
		if err := rows.Scan(&l.ID, &l.Name, &aliases); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(aliases), &l.Aliases); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Parts returns the cached part vocabulary.
func (s *Store) Parts() ([]refdata.Part, error) {
	rows, err := s.db.Query(`SELECT part_number, name, aliases FROM ref_parts ORDER BY part_number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []refdata.Part
	for rows.Next() {
		var p refdata.Part
		var aliases string
		if err := rows.Scan(&p.PartNumber, &p.Name, &aliases); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(aliases), &p.Aliases); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Units returns the cached unit vocabulary.
func (s *Store) Units() ([]refdata.Unit, error) {
	rows, err := s.db.Query(`SELECT name, language, aliases FROM ref_units ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []refdata.Unit
	for rows.Next() {
		var u refdata.Unit
		var aliases string
		if err := rows.Scan(&u.Name, &u.Language, &aliases); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(aliases), &u.Aliases); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Sync state (etags, cursors)

// GetSyncState reads a sync-state value; missing keys return "".
func (s *Store) GetSyncState(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM sync_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSyncState writes a sync-state value.
func (s *Store) SetSyncState(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO sync_state (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
