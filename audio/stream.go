package audio

import "math"

// Resampler is a stateful linear resampler for chunked streams: unlike the
// one-shot Resample, it carries the interpolation phase and the last sample
// across calls, so feeding audio chunk by chunk loses no samples and adds
// no discontinuities at non-integer ratios (44.1 kHz → 16 kHz).
type Resampler struct {
	step   float64 // input samples per output sample
	pos    float64 // next output position, relative to the retained sample
	last   float32
	primed bool
	same   bool
}

// NewResampler converts fromRate to toRate. Equal or non-positive rates
// pass audio through unchanged.
func NewResampler(fromRate, toRate int) *Resampler {
	if fromRate <= 0 || toRate <= 0 || fromRate == toRate {
		return &Resampler{same: true}
	}
	return &Resampler{step: float64(fromRate) / float64(toRate)}
}

// Process consumes one chunk and returns the resampled output (possibly
// empty for very small chunks).
func (r *Resampler) Process(in []float32) []float32 {
	if r.same || len(in) == 0 {
		return in
	}
	// work[0] is the last sample of the previous chunk so interpolation
	// spans the boundary.
	var work []float32
	if r.primed {
		work = make([]float32, 0, len(in)+1)
		work = append(work, r.last)
		work = append(work, in...)
	} else {
		work = in
		r.primed = true
	}
	out := make([]float32, 0, int(float64(len(in))/r.step)+2)
	pos := r.pos
	for pos+1 < float64(len(work)) {
		j := int(pos)
		frac := float32(pos - float64(j))
		out = append(out, work[j]*(1-frac)+work[j+1]*frac)
		pos += r.step
	}
	// Exact hit on the final sample (frac 0) is still emittable.
	if pos == float64(len(work)-1) {
		out = append(out, work[len(work)-1])
		pos += r.step
	}
	r.last = work[len(work)-1]
	r.pos = pos - float64(len(work)-1)
	return out
}

// HighPassFilter is a stateful first-order high-pass (DC/rumble block)
// whose state carries across chunks (§8.3 step 3).
type HighPassFilter struct {
	alpha   float32
	prevIn  float32
	prevOut float32
	primed  bool
}

// NewHighPass builds a filter for the given rate and cutoff. A non-positive
// cutoff yields a pass-through filter.
func NewHighPass(sampleRate int, cutoffHz float64) *HighPassFilter {
	if sampleRate <= 0 || cutoffHz <= 0 {
		return &HighPassFilter{alpha: 1, primed: true}
	}
	rc := 1 / (2 * math.Pi * cutoffHz)
	dt := 1 / float64(sampleRate)
	return &HighPassFilter{alpha: float32(rc / (rc + dt))}
}

// Process filters one chunk in place and returns it.
func (f *HighPassFilter) Process(in []float32) []float32 {
	if f.alpha == 1 {
		return in
	}
	for i, s := range in {
		if !f.primed {
			f.prevIn = s
			f.prevOut = 0
			f.primed = true
			in[i] = 0
			continue
		}
		out := f.alpha * (f.prevOut + s - f.prevIn)
		f.prevIn = s
		f.prevOut = out
		in[i] = out
	}
	return in
}
