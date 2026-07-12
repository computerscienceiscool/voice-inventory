package audio

import (
	"math"
	"testing"
)

// Chunked resampling must lose no samples and add no discontinuities at
// non-integer ratios, unlike calling the one-shot Resample per chunk.
func TestResamplerChunkedLength(t *testing.T) {
	const from, to = 44100, 16000
	in := sine(440, from, from*3, 0.5) // 3 s
	r := NewResampler(from, to)
	var out []float32
	for i := 0; i < len(in); i += 1000 {
		end := i + 1000
		if end > len(in) {
			end = len(in)
		}
		out = append(out, r.Process(in[i:end])...)
	}
	want := len(in) * to / from
	if diff := len(out) - want; diff < -2 || diff > 2 {
		t.Errorf("chunked output = %d samples, want ≈%d (drift %d)", len(out), want, diff)
	}
}

func TestResamplerMatchesOneShot(t *testing.T) {
	const from, to = 48000, 16000
	in := sine(440, from, from, 0.5)
	oneShot := Resample(in, from, to)

	r := NewResampler(from, to)
	var chunked []float32
	for i := 0; i < len(in); i += 777 { // awkward chunk size on purpose
		end := i + 777
		if end > len(in) {
			end = len(in)
		}
		chunked = append(chunked, r.Process(in[i:end])...)
	}
	if d := len(chunked) - len(oneShot); d < -2 || d > 2 {
		t.Fatalf("lengths differ: chunked %d vs one-shot %d", len(chunked), len(oneShot))
	}
	n := len(chunked)
	if len(oneShot) < n {
		n = len(oneShot)
	}
	for i := 0; i < n; i++ {
		if math.Abs(float64(chunked[i]-oneShot[i])) > 1e-4 {
			t.Fatalf("sample %d: chunked %v vs one-shot %v", i, chunked[i], oneShot[i])
		}
	}
}

func TestResamplerPassthrough(t *testing.T) {
	r := NewResampler(16000, 16000)
	in := []float32{1, 2, 3}
	out := r.Process(in)
	if len(out) != 3 || out[0] != 1 {
		t.Errorf("passthrough wrong: %v", out)
	}
}

// The stateful filter over chunks must match the one-shot filter.
func TestHighPassFilterChunkedMatchesOneShot(t *testing.T) {
	in := make([]float32, 16000)
	for i := range in {
		in[i] = 0.5 + float32(0.3*math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	oneShot := append([]float32(nil), in...)
	HighPass(oneShot, 16000, 100)

	f := NewHighPass(16000, 100)
	chunked := append([]float32(nil), in...)
	for i := 0; i < len(chunked); i += 333 {
		end := i + 333
		if end > len(chunked) {
			end = len(chunked)
		}
		f.Process(chunked[i:end])
	}
	for i := range chunked {
		if math.Abs(float64(chunked[i]-oneShot[i])) > 1e-5 {
			t.Fatalf("sample %d: chunked %v vs one-shot %v", i, chunked[i], oneShot[i])
		}
	}
	// DC is removed
	var mean float64
	for _, s := range chunked[8000:] {
		mean += float64(s)
	}
	if mean /= 8000; math.Abs(mean) > 0.01 {
		t.Errorf("DC not removed: %v", mean)
	}
}

func TestHighPassDisabled(t *testing.T) {
	f := NewHighPass(16000, 0)
	in := []float32{0.5, 0.5}
	out := f.Process(in)
	if out[0] != 0.5 || out[1] != 0.5 {
		t.Errorf("disabled filter must pass through: %v", out)
	}
}
