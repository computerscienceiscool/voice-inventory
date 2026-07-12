package integration

import (
	"testing"

	"github.com/computerscienceiscool/voice-inventory/audio"
	"github.com/computerscienceiscool/voice-inventory/lang"
	"github.com/computerscienceiscool/voice-inventory/parser"
	"github.com/computerscienceiscool/voice-inventory/refdata"
	"github.com/computerscienceiscool/voice-inventory/vad"
)

// These measure the on-device work that surrounds whisper.cpp inference —
// the parser, VAD, and resampling that must add negligible latency on top
// of ASR to hit the §8.4 utterance-end → readback targets. whisper.cpp
// dominates; everything here should stay in the microseconds.

func benchResolver() *refdata.Index {
	locs := make([]refdata.Location, 200)
	for i := range locs {
		locs[i] = refdata.Location{ID: "LOC", Name: "Bin X", Aliases: []string{"A-14"}}
	}
	parts := make([]refdata.Part, 500)
	for i := range parts {
		parts[i] = refdata.Part{PartNumber: "PN", Name: "RJ45 connector"}
	}
	return refdata.NewIndex(locs, parts)
}

func BenchmarkParseEnglish(b *testing.B) {
	opts := parser.Options{Lang: lang.English, Resolver: benchResolver()}
	text := "Twelve boxes of RJ45 connectors in bin A-14, three have water damage"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.Parse(text, opts)
	}
}

func BenchmarkParseSpanish(b *testing.B) {
	opts := parser.Options{Lang: lang.Spanish, Resolver: benchResolver()}
	text := "cuarenta carretes de Cat6 en el bin C-7, tres tienen daño de agua"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.Parse(text, opts)
	}
}

// BenchmarkVADTrim measures segmenting a 5 s utterance (the §8.4 reference
// length): 0.5 s lead-in silence, 4 s tone, 0.5 s trailing silence.
func BenchmarkVADTrim(b *testing.B) {
	var pcm []float32
	pcm = append(pcm, make([]float32, 8000)...)
	pcm = append(pcm, tone(4.0)...)
	pcm = append(pcm, make([]float32, 8000)...)
	cfg := vad.Config{SampleRate: 16000}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = vad.Trim(pcm, cfg)
	}
}

// BenchmarkResample48kTo16k measures downsampling 5 s of 48 kHz audio.
func BenchmarkResample48kTo16k(b *testing.B) {
	in := make([]float32, 48000*5)
	for i := range in {
		in[i] = float32(0.2 * float64(i%97))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = audio.Resample(in, 48000, 16000)
	}
}
