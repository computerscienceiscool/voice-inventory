package vad

import (
	"math"
	"testing"
)

const rate = 16000

func sine(freq float64, n int, amp float64) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*freq*float64(i)/rate))
	}
	return out
}

func silence(n int) []float32 { return make([]float32, n) }

// noiseFloor produces a quiet hum below the activation threshold.
func noiseFloor(n int) []float32 {
	return sine(120, n, 0.002)
}

func collect(d *Detector, samples []float32, chunk int) []Event {
	var events []Event
	for i := 0; i < len(samples); i += chunk {
		end := i + chunk
		if end > len(samples) {
			end = len(samples)
		}
		events = append(events, d.Process(samples[i:end])...)
	}
	return events
}

func utterances(events []Event) []Event {
	var out []Event
	for _, e := range events {
		if e.Kind == EventUtterance {
			out = append(out, e)
		}
	}
	return out
}

func TestDetectsUtterance(t *testing.T) {
	d := NewDetector(Config{SampleRate: rate})
	var stream []float32
	stream = append(stream, noiseFloor(rate/2)...)   // 0.5 s quiet
	stream = append(stream, sine(440, rate, 0.3)...) // 1 s speech-ish tone
	stream = append(stream, noiseFloor(rate)...)     // 1 s quiet → closes
	events := collect(d, stream, 512)

	started := false
	for _, e := range events {
		if e.Kind == EventSpeechStart {
			started = true
		}
	}
	if !started {
		t.Fatal("no SpeechStart event")
	}
	utts := utterances(events)
	if len(utts) != 1 {
		t.Fatalf("utterances = %d, want 1", len(utts))
	}
	u := utts[0]
	if u.Truncated {
		t.Error("should not be truncated")
	}
	// Utterance ≈ 1 s of tone + pre-roll + hangover; well under the raw 2.5 s.
	if len(u.Utterance) < rate*3/4 || len(u.Utterance) > rate*2 {
		t.Errorf("utterance length = %d samples (%.2fs)", len(u.Utterance),
			float64(len(u.Utterance))/rate)
	}
	// Level events flow continuously.
	levels := 0
	for _, e := range events {
		if e.Kind == EventLevel {
			levels++
		}
	}
	if levels < 50 {
		t.Errorf("expected many level events, got %d", levels)
	}
}

func TestIgnoresSilence(t *testing.T) {
	d := NewDetector(Config{SampleRate: rate})
	events := collect(d, noiseFloor(rate*3), 480)
	if len(utterances(events)) != 0 {
		t.Error("silence should produce no utterances")
	}
	if evs := d.Flush(); len(utterances(evs)) != 0 {
		t.Error("flush of silence should produce no utterances")
	}
}

func TestRejectsHiss(t *testing.T) {
	// Alternating-sign "noise" has ZCR ≈ 1, far above MaxZCR.
	d := NewDetector(Config{SampleRate: rate})
	hiss := make([]float32, rate*2)
	for i := range hiss {
		if i%2 == 0 {
			hiss[i] = 0.3
		} else {
			hiss[i] = -0.3
		}
	}
	events := collect(d, hiss, 480)
	events = append(events, d.Flush()...)
	if len(utterances(events)) != 0 {
		t.Error("high-ZCR hiss should not trigger an utterance")
	}
}

func TestMaxUtteranceCap(t *testing.T) {
	d := NewDetector(Config{SampleRate: rate, MaxUtteranceMS: 2000})
	var stream []float32
	stream = append(stream, noiseFloor(rate/2)...)
	stream = append(stream, sine(440, rate*4, 0.3)...) // 4 s continuous tone
	events := collect(d, stream, 512)
	utts := utterances(events)
	if len(utts) == 0 {
		t.Fatal("expected a truncated utterance")
	}
	if !utts[0].Truncated {
		t.Error("utterance should be marked truncated")
	}
	if len(utts[0].Utterance) > rate*5/2 {
		t.Errorf("capped utterance too long: %d samples", len(utts[0].Utterance))
	}
}

func TestFlushEndsUtterance(t *testing.T) {
	d := NewDetector(Config{SampleRate: rate})
	var stream []float32
	stream = append(stream, noiseFloor(rate/2)...)
	stream = append(stream, sine(440, rate, 0.3)...) // speech, never goes quiet
	events := collect(d, stream, 512)
	if len(utterances(events)) != 0 {
		t.Fatal("utterance should still be open")
	}
	events = d.Flush()
	utts := utterances(events)
	if len(utts) != 1 {
		t.Fatalf("flush should deliver the open utterance, got %d", len(utts))
	}
	// detector reusable after flush
	events = collect(d, append(noiseFloor(rate/2), append(sine(440, rate, 0.3), noiseFloor(rate)...)...), 512)
	if len(utterances(events)) != 1 {
		t.Error("detector should be reusable after Flush")
	}
}

func TestTrim(t *testing.T) {
	var pcm []float32
	pcm = append(pcm, noiseFloor(rate)...)       // 1 s leading quiet
	pcm = append(pcm, sine(440, rate/2, 0.3)...) // 0.5 s speech
	pcm = append(pcm, noiseFloor(rate*2)...)     // 2 s trailing quiet
	trimmed, ok := Trim(pcm, Config{SampleRate: rate})
	if !ok {
		t.Fatal("expected speech to be found")
	}
	if len(trimmed) >= len(pcm) {
		t.Error("trim should shrink the buffer")
	}
	if len(trimmed) < rate/4 || len(trimmed) > rate*3/2 {
		t.Errorf("trimmed length = %.2fs", float64(len(trimmed))/rate)
	}
	if _, ok := Trim(noiseFloor(rate*2), Config{SampleRate: rate}); ok {
		t.Error("pure quiet should find no speech")
	}
}

// A held push-to-talk button with a mid-sentence pause must keep BOTH
// halves of the speech, not just the first.
func TestTrimKeepsSpeechAfterPause(t *testing.T) {
	var pcm []float32
	pcm = append(pcm, noiseFloor(rate/2)...)
	pcm = append(pcm, sine(440, rate/2, 0.3)...) // "twelve boxes…"
	pcm = append(pcm, noiseFloor(rate)...)       // 1 s pause (> EndSilenceMS)
	pcm = append(pcm, sine(440, rate/2, 0.3)...) // "…in bin A-14"
	pcm = append(pcm, noiseFloor(rate/2)...)
	trimmed, ok := Trim(pcm, Config{SampleRate: rate})
	if !ok {
		t.Fatal("speech not found")
	}
	// Both halves ≈ 1 s of tone total (plus pre-roll/hangover); losing the
	// second half would leave well under 0.8 s.
	if len(trimmed) < rate*9/10 {
		t.Errorf("trimmed = %.2fs; the post-pause speech was dropped",
			float64(len(trimmed))/rate)
	}
}

// A constant low-frequency hum above MinRMS (HVAC, compressor) must not
// lock the detector into endless utterances: the ZCR gate rejects pure
// tones and the adaptive floor absorbs steady ambience.
func TestHumDoesNotTrigger(t *testing.T) {
	d := NewDetector(Config{SampleRate: rate})
	hum := sine(100, rate*10, 0.03) // 10 s at rms ≈ 0.021, above MinRMS
	events := collect(d, hum, 480)
	events = append(events, d.Flush()...)
	if n := len(utterances(events)); n != 0 {
		t.Errorf("hum produced %d utterances, want 0", n)
	}
	// Real speech right after the hum must still be detected.
	d2 := NewDetector(Config{SampleRate: rate})
	var stream []float32
	stream = append(stream, sine(100, rate*3, 0.03)...) // hum
	stream = append(stream, sine(440, rate, 0.3)...)    // speech-like tone
	stream = append(stream, noiseFloor(rate)...)        // quiet
	events = collect(d2, stream, 480)
	events = append(events, d2.Flush()...)
	if n := len(utterances(events)); n != 1 {
		t.Errorf("speech after hum: %d utterances, want 1", n)
	}
}
