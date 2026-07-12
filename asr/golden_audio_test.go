package asr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/computerscienceiscool/voice-inventory/audio"
	"github.com/computerscienceiscool/voice-inventory/lang"
	"github.com/computerscienceiscool/voice-inventory/parser"
	"github.com/computerscienceiscool/voice-inventory/vad"
)

// goldenAudioCase pairs a recorded utterance with its expected parse.
type goldenAudioCase struct {
	WAV      string  `json:"wav"`
	Lang     string  `json:"lang"`
	Quantity float64 `json:"quantity"`
	Item     string  `json:"item"`     // substring match, case-insensitive
	Location string  `json:"location"` // exact location_text
}

// TestGoldenAudio runs the end-to-end ASR→parse regression suite (spec
// §15) over recorded warehouse utterances. It needs a whisper.cpp binary
// and weights, so it activates only when the environment provides them:
//
//	VINV_WHISPER_BIN=…/whisper-cli VINV_WHISPER_MODEL=…/ggml-small-q5_1.bin \
//	  go test ./asr -run TestGoldenAudio -v
//
// Cases live in asr/testdata/golden_audio/cases.json next to their WAVs.
func TestGoldenAudio(t *testing.T) {
	bin := os.Getenv("VINV_WHISPER_BIN")
	model := os.Getenv("VINV_WHISPER_MODEL")
	if bin == "" || model == "" {
		t.Skip("set VINV_WHISPER_BIN and VINV_WHISPER_MODEL to run the golden-audio suite")
	}
	data, err := os.ReadFile(filepath.Join("testdata", "golden_audio", "cases.json"))
	if err != nil {
		t.Skipf("no golden audio cases: %v", err)
	}
	var cases []goldenAudioCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	tr := NewExec(bin)
	if err := tr.LoadModel(model, ModelOpts{}); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.WAV, func(t *testing.T) {
			f, err := os.Open(filepath.Join("testdata", "golden_audio", c.WAV))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			samples, rate, err := audio.DecodeWAV(f)
			if err != nil {
				t.Fatal(err)
			}
			pcm := audio.Resample(samples, rate, audio.WhisperRate)
			if trimmed, ok := vad.Trim(pcm, vad.Config{SampleRate: audio.WhisperRate}); ok {
				pcm = trimmed
			}
			res, err := tr.Transcribe(context.Background(), pcm, Lang(c.Lang))
			if err != nil {
				t.Fatal(err)
			}
			r := parser.Parse(res.Text, parser.Options{Lang: lang.Code(c.Lang)})
			if r.Parsed.Quantity == nil || *r.Parsed.Quantity != c.Quantity {
				t.Errorf("quantity = %v, want %v (transcript %q)",
					r.Parsed.Quantity, c.Quantity, res.Text)
			}
			if !strings.Contains(strings.ToLower(r.Parsed.ItemText), strings.ToLower(c.Item)) {
				t.Errorf("item = %q, want ~%q (transcript %q)",
					r.Parsed.ItemText, c.Item, res.Text)
			}
			if r.Parsed.LocationText != c.Location {
				t.Errorf("location = %q, want %q (transcript %q)",
					r.Parsed.LocationText, c.Location, res.Text)
			}
		})
	}
}
