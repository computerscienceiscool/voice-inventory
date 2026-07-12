// Command vinv is the desktop driver for the voice-inventory core: it
// parses utterances, runs the full capture pipeline against a WAV file via
// whisper.cpp, manages the local queue, syncs to a backend, and can serve
// an in-memory mock backend for end-to-end testing.
//
// Usage:
//
//	vinv parse       [-lang en] [-db path] "twelve boxes of RJ45 in bin A-14"
//	vinv transcript  -db path [-lang en] [-confirm] "utterance text"
//	vinv capture     -db path -wav clip.wav -whisper ./whisper-cli -model w.bin [-lang auto] [-confirm]
//	vinv add         -db path -item X [-qty N] [-unit u] [-loc L] [-desc D] [-confirm]
//	vinv list        -db path [-status confirmed] [-limit 20]
//	vinv confirm     -db path <id> ...
//	vinv reject      -db path <id> ...
//	vinv edit        -db path -id ID -field location -value B-2
//	vinv sync        -db path -endpoint URL [-token t] [-device d] [-insecure] [-mode push|pull|all]
//	vinv refdata     -db path [-locations f.json] [-parts f.json] [-units f.json]
//	vinv purge-audio -db path -audio-dir dir [-keep-days 7]
//	vinv stats       -db path
//	vinv mockserver  [-addr :8873] [-refdata f.json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/computerscienceiscool/voice-inventory/asr"
	"github.com/computerscienceiscool/voice-inventory/audio"
	"github.com/computerscienceiscool/voice-inventory/config"
	"github.com/computerscienceiscool/voice-inventory/export"
	"github.com/computerscienceiscool/voice-inventory/lang"
	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/parser"
	"github.com/computerscienceiscool/voice-inventory/refdata"
	"github.com/computerscienceiscool/voice-inventory/session"
	"github.com/computerscienceiscool/voice-inventory/store"
	"github.com/computerscienceiscool/voice-inventory/syncer"
	"github.com/computerscienceiscool/voice-inventory/vad"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vinv:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: vinv <parse|transcript|capture|add|list|confirm|reject|edit|sync|refdata|purge-audio|stats|mockserver> …")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "parse":
		return cmdParse(rest)
	case "transcript":
		return cmdTranscript(rest)
	case "capture":
		return cmdCapture(rest)
	case "add":
		return cmdAdd(rest)
	case "list":
		return cmdList(rest)
	case "confirm":
		return cmdSetStatus(rest, true)
	case "reject":
		return cmdSetStatus(rest, false)
	case "edit":
		return cmdEdit(rest)
	case "sync":
		return cmdSync(rest)
	case "refdata":
		return cmdRefData(rest)
	case "purge-audio":
		return cmdPurgeAudio(rest)
	case "stats":
		return cmdStats(rest)
	case "export":
		return cmdExport(rest)
	case "mockserver":
		return cmdMockServer(rest)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// textArgs joins positional args into the utterance, refusing stray flags:
// Go's flag parsing stops at the first positional argument, so a trailing
// "-lang es" would otherwise silently become part of the spoken text.
func textArgs(fs *flag.FlagSet) (string, error) {
	for _, a := range fs.Args() {
		if strings.HasPrefix(a, "-") {
			return "", fmt.Errorf("flag %q appears after the utterance text; put flags first", a)
		}
	}
	return strings.Join(fs.Args(), " "), nil
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func openStore(path string) (*store.Store, error) {
	if path == "" {
		return nil, fmt.Errorf("-db is required")
	}
	return store.Open(path)
}

func resolverFrom(st *store.Store) (*refdata.Index, error) {
	if st == nil {
		return nil, nil
	}
	locs, err := st.Locations()
	if err != nil {
		return nil, err
	}
	parts, err := st.Parts()
	if err != nil {
		return nil, err
	}
	return refdata.NewIndex(locs, parts), nil
}

// --- parse -----------------------------------------------------------------

func cmdParse(args []string) error {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	langFlag := fs.String("lang", "en", "language (en|es)")
	db := fs.String("db", "", "optional store for reference-data resolution")
	multi := fs.Bool("multi", false, "enable multi-item splitting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text, err := textArgs(fs)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("no utterance text given")
	}
	opts := parser.Options{Lang: lang.Code(*langFlag), MultiItem: *multi}
	if *db != "" {
		st, err := openStore(*db)
		if err != nil {
			return err
		}
		defer st.Close()
		if opts.Resolver, err = resolverFrom(st); err != nil {
			return err
		}
	}
	return printJSON(parser.ParseAll(text, opts))
}

// --- capture pipeline -------------------------------------------------------

// cliListener prints session events for interactive use.
type cliListener struct{}

func (c *cliListener) OnState(s session.State) {}
func (c *cliListener) OnLevel(float64)         {}
func (c *cliListener) OnSpeechStart()          {}
func (c *cliListener) OnReadback(rb session.Readback) {
	fmt.Println("readback:", rb.Text)
	if len(rb.Doubtful) > 0 {
		fmt.Println("doubtful:", strings.Join(rb.Doubtful, ", "))
	}
}
func (c *cliListener) OnSaved(id string, st observation.Status) {
	fmt.Printf("saved %s (%s)\n", id, st)
}
func (c *cliListener) OnDiscarded(id string)   { fmt.Println("discarded", id) }
func (c *cliListener) OnError(msg string)      { fmt.Fprintln(os.Stderr, "error:", msg) }
func (c *cliListener) OnSuggestion(msg string) { fmt.Println("suggestion:", msg) }

func newSession(st *store.Store, langCode string, tr asr.Transcriber, audioDir string) (*session.Session, error) {
	cfg := config.Default()
	cfg.Language = langCode
	host, _ := os.Hostname()
	cfg.DeviceID = "cli-" + host
	cfg.OperatorID = os.Getenv("USER")
	return session.New(cfg, session.Deps{
		Store:       st,
		Transcriber: tr,
		Listener:    &cliListener{},
		AudioDir:    audioDir,
	})
}

func cmdTranscript(args []string) error {
	fs := flag.NewFlagSet("transcript", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	langFlag := fs.String("lang", "en", "language (en|es)")
	confirm := fs.Bool("confirm", false, "confirm immediately")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text, err := textArgs(fs)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("no transcript text given")
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	s, err := newSession(st, *langFlag, &asr.Mock{}, "")
	if err != nil {
		return err
	}
	s.Arm()
	s.HandleTranscript(text, asr.Result{Text: text, Language: *langFlag, Confidence: 1}, nil)
	return finishPending(s, *confirm)
}

func finishPending(s *session.Session, confirm bool) error {
	p := s.Pending()
	if p == nil {
		return nil
	}
	if err := printJSON(p); err != nil {
		return err
	}
	if confirm {
		return s.Confirm()
	}
	fmt.Printf("left as draft %s (confirm with: vinv confirm -db … %s)\n", p.ID, p.ID)
	return nil
}

func cmdCapture(args []string) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	wav := fs.String("wav", "", "input WAV file")
	whisperBin := fs.String("whisper", "whisper-cli", "whisper.cpp CLI binary")
	model := fs.String("model", "", "ggml weights path")
	langFlag := fs.String("lang", "auto", "language (en|es|auto)")
	threads := fs.Int("threads", 0, "whisper threads")
	confirm := fs.Bool("confirm", false, "confirm immediately")
	audioDir := fs.String("audio-dir", "", "retain clip into this directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *wav == "" || *model == "" {
		return fmt.Errorf("-wav and -model are required")
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	f, err := os.Open(*wav)
	if err != nil {
		return err
	}
	samples, rate, err := audio.DecodeWAV(f)
	_ = f.Close()
	if err != nil {
		return err
	}
	pcm := audio.Resample(samples, rate, audio.WhisperRate)
	if trimmed, ok := vad.Trim(pcm, vad.Config{SampleRate: audio.WhisperRate}); ok {
		pcm = trimmed
	}

	tr := asr.NewExec(*whisperBin)
	if err := tr.LoadModel(*model, asr.ModelOpts{Threads: *threads}); err != nil {
		return err
	}
	s, err := newSession(st, *langFlag, tr, *audioDir)
	if err != nil {
		return err
	}
	s.Arm()
	res, err := tr.Transcribe(context.Background(), pcm, asr.Lang(*langFlag))
	if err != nil {
		return err
	}
	fmt.Println("transcript:", res.Text)
	s.HandleTranscript(res.Text, res, pcm)
	return finishPending(s, *confirm)
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	item := fs.String("item", "", "item text")
	qty := fs.Float64("qty", -1, "quantity (-1 = none)")
	unit := fs.String("unit", "", "unit")
	loc := fs.String("loc", "", "location text")
	desc := fs.String("desc", "", "description")
	langFlag := fs.String("lang", "en", "language")
	confirm := fs.Bool("confirm", false, "confirm immediately")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	s, err := newSession(st, *langFlag, &asr.Mock{}, "")
	if err != nil {
		return err
	}
	p := observation.Parsed{ItemText: *item, LocationText: *loc}
	if *qty >= 0 {
		p.Quantity = qty
	}
	if *unit != "" {
		p.Unit = unit
	}
	if *desc != "" {
		p.Description = desc
	}
	id, err := s.AddManual(p, *langFlag, *confirm)
	if err != nil {
		return err
	}
	fmt.Println(id)
	return nil
}

// --- queue management --------------------------------------------------------

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	status := fs.String("status", "", "filter by status")
	limit := fs.Int("limit", 0, "max records")
	needsReview := fs.Bool("needs-review", false, "only flagged records")
	syncRejected := fs.Bool("sync-rejected", false, "only records the backend refused")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *status != "" && !observation.Status(*status).Valid() {
		return fmt.Errorf("unknown -status %q (want draft, confirmed, synced, or rejected)", *status)
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	f := store.Filter{Status: observation.Status(*status), Limit: *limit}
	if *needsReview {
		v := true
		f.NeedsReview = &v
	}
	if *syncRejected {
		v := true
		f.SyncRejected = &v
	}
	obs, err := st.List(f)
	if err != nil {
		return err
	}
	if obs == nil {
		obs = []*observation.Observation{}
	}
	return printJSON(obs)
}

func cmdSetStatus(args []string, confirm bool) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("record id(s) required")
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, id := range fs.Args() {
		var err error
		if confirm {
			err = st.Confirm(id)
		} else {
			err = st.Reject(id)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		fmt.Println("ok", id)
	}
	return nil
}

func cmdEdit(args []string) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	id := fs.String("id", "", "record id")
	field := fs.String("field", "", "location|quantity|item|unit|description")
	value := fs.String("value", "", "new value")
	langFlag := fs.String("lang", "en", "language")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" || *field == "" {
		return fmt.Errorf("-id and -field are required")
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	s, err := newSession(st, *langFlag, &asr.Mock{}, "")
	if err != nil {
		return err
	}
	if err := s.EditRecord(*id, *field, *value); err != nil {
		return err
	}
	got, err := st.Get(*id)
	if err != nil {
		return err
	}
	return printJSON(got)
}

// --- sync ---------------------------------------------------------------------

func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	endpoint := fs.String("endpoint", "", "backend base URL")
	token := fs.String("token", "", "bearer token (prefer VINV_TOKEN env — argv is visible in ps)")
	device := fs.String("device", "cli", "device id")
	insecure := fs.Bool("insecure", false, "allow http:// (development)")
	mode := fs.String("mode", "all", "push|pull|all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *mode {
	case "push", "pull", "all":
	default:
		return fmt.Errorf("unknown -mode %q (want push, pull, or all)", *mode)
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	tok := *token
	if tok == "" {
		tok = os.Getenv("VINV_TOKEN")
	}
	h, err := syncer.NewHTTP(st, syncer.Options{
		BaseURL: *endpoint, Token: tok, DeviceID: *device,
		AllowInsecure: *insecure,
	})
	if err != nil {
		return err
	}
	ctx := context.Background()
	if *mode == "push" || *mode == "all" {
		report, err := h.Push(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("pushed %d record(s) in %d batch(es)\n", report.Pushed, report.Batches)
		for _, r := range report.Rejected {
			fmt.Printf("rejected %s: %s\n", r.ID, r.Reason)
		}
	}
	if *mode == "pull" || *mode == "all" {
		report, err := h.PullRefData(ctx)
		if err != nil {
			return err
		}
		if report.NotModified {
			fmt.Println("reference data unchanged")
		} else {
			fmt.Printf("pulled %d locations, %d parts, %d units\n",
				report.Locations, report.Parts, report.Units)
		}
	}
	return nil
}

func cmdRefData(args []string) error {
	fs := flag.NewFlagSet("refdata", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	locFile := fs.String("locations", "", "locations JSON file")
	partFile := fs.String("parts", "", "parts JSON file")
	unitFile := fs.String("units", "", "units JSON file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	if *locFile != "" {
		var locs []refdata.Location
		if err := readJSON(*locFile, &locs); err != nil {
			return err
		}
		if err := st.ReplaceLocations(locs); err != nil {
			return err
		}
		fmt.Printf("imported %d locations\n", len(locs))
	}
	if *partFile != "" {
		var parts []refdata.Part
		if err := readJSON(*partFile, &parts); err != nil {
			return err
		}
		if err := st.ReplaceParts(parts); err != nil {
			return err
		}
		fmt.Printf("imported %d parts\n", len(parts))
	}
	if *unitFile != "" {
		var units []refdata.Unit
		if err := readJSON(*unitFile, &units); err != nil {
			return err
		}
		if err := st.ReplaceUnits(units); err != nil {
			return err
		}
		fmt.Printf("imported %d units\n", len(units))
	}
	return nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func cmdPurgeAudio(args []string) error {
	fs := flag.NewFlagSet("purge-audio", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	audioDir := fs.String("audio-dir", "", "clip directory")
	keepDays := fs.Int("keep-days", 7, "days to keep after sync")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *audioDir == "" {
		return fmt.Errorf("-audio-dir is required")
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	cfg := config.Default()
	cfg.Retention.KeepDays = *keepDays
	s, err := session.New(cfg, session.Deps{
		Store: st, Transcriber: &asr.Mock{}, AudioDir: *audioDir,
	})
	if err != nil {
		return err
	}
	n, err := s.PurgeAudio()
	if err != nil {
		return err
	}
	fmt.Printf("purged %d clip(s)\n", n)
	return nil
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	status := fs.String("status", "", "filter by status")
	out := fs.String("o", "", "output CSV file (default stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *status != "" && !observation.Status(*status).Valid() {
		return fmt.Errorf("unknown -status %q", *status)
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	obs, err := st.List(store.Filter{Status: observation.Status(*status)})
	if err != nil {
		return err
	}
	w := io.Writer(os.Stdout)
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	if err := export.CSV(w, obs); err != nil {
		return err
	}
	if *out != "" {
		fmt.Printf("exported %d record(s) to %s\n", len(obs), *out)
	}
	return nil
}

func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	db := fs.String("db", "", "store path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	counts, err := st.CountsByStatus()
	if err != nil {
		return err
	}
	return printJSON(counts)
}
