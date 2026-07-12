package audio

import (
	"bytes"
	"math"
	"testing"
)

func sine(freq float64, rate, n int, amp float64) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
	}
	return out
}

func TestResample(t *testing.T) {
	in := sine(440, 48000, 48000, 0.5) // 1 s at 48 kHz
	out := Resample(in, 48000, 16000)
	if len(out) < 15900 || len(out) > 16100 {
		t.Fatalf("resampled length = %d, want ≈16000", len(out))
	}
	// energy should be roughly preserved
	if r := RMS(out); math.Abs(r-RMS(in)) > 0.02 {
		t.Errorf("rms drift: %v vs %v", r, RMS(in))
	}
	// same-rate passthrough
	if got := Resample(in, 16000, 16000); len(got) != len(in) {
		t.Error("same-rate should pass through")
	}
	if got := Resample(nil, 48000, 16000); got != nil {
		t.Error("empty input should stay empty")
	}
}

func TestMonoFromInterleaved(t *testing.T) {
	stereo := []float32{1, 0, 1, 0, -1, 0}
	mono := MonoFromInterleaved(stereo, 2)
	if len(mono) != 3 || mono[0] != 0.5 || mono[2] != -0.5 {
		t.Errorf("downmix wrong: %v", mono)
	}
	if got := MonoFromInterleaved(stereo, 1); len(got) != 6 {
		t.Error("mono passthrough wrong")
	}
}

func TestInt16RoundTrip(t *testing.T) {
	in := []float32{0, 0.5, -0.5, 0.999, -1}
	b := Float32ToInt16(in)
	out := Int16ToFloat32(b)
	if len(out) != len(in) {
		t.Fatalf("length mismatch: %d", len(out))
	}
	for i := range in {
		if math.Abs(float64(out[i]-in[i])) > 0.001 {
			t.Errorf("sample %d: %v vs %v", i, out[i], in[i])
		}
	}
	// clamping
	b = Float32ToInt16([]float32{2, -2})
	out = Int16ToFloat32(b)
	if out[0] < 0.99 || out[1] > -0.99 {
		t.Errorf("clamping failed: %v", out)
	}
}

func TestHighPassRemovesDC(t *testing.T) {
	in := make([]float32, 16000)
	for i := range in {
		in[i] = 0.5 // pure DC
	}
	out := HighPass(in, 16000, 100)
	var mean float64
	for _, s := range out[8000:] { // after settling
		mean += float64(s)
	}
	mean /= 8000
	if math.Abs(mean) > 0.01 {
		t.Errorf("DC not removed: mean %v", mean)
	}
}

func TestWAVRoundTrip(t *testing.T) {
	in := sine(440, 16000, 8000, 0.4)
	var buf bytes.Buffer
	if err := EncodeWAV16(&buf, in, 16000); err != nil {
		t.Fatal(err)
	}
	out, rate, err := DecodeWAV(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 16000 {
		t.Errorf("rate = %d", rate)
	}
	if len(out) != len(in) {
		t.Fatalf("length = %d, want %d", len(out), len(in))
	}
	for i := 0; i < len(in); i += 500 {
		if math.Abs(float64(out[i]-in[i])) > 0.001 {
			t.Errorf("sample %d: %v vs %v", i, out[i], in[i])
		}
	}
}

func TestDecodeWAVErrors(t *testing.T) {
	if _, _, err := DecodeWAV(bytes.NewReader([]byte("not a wav file at all"))); err == nil {
		t.Error("garbage should fail")
	}
	if _, _, err := DecodeWAV(bytes.NewReader(nil)); err == nil {
		t.Error("empty should fail")
	}
}

func TestRMS(t *testing.T) {
	if RMS(nil) != 0 {
		t.Error("empty RMS should be 0")
	}
	full := make([]float32, 100)
	for i := range full {
		full[i] = 1
	}
	if r := RMS(full); math.Abs(r-1) > 1e-9 {
		t.Errorf("RMS of ones = %v", r)
	}
}
