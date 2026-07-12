// Package vad implements the energy + zero-crossing voice-activity
// detector for the MVP (spec §8.3 step 2): it segments utterances out of a
// live sample stream, trims leading/trailing silence, keeps a pre-roll so
// soft first syllables survive, and caps utterance length.
package vad

import (
	"math"
)

// Config tunes the detector. Zero values take defaults.
type Config struct {
	SampleRate      int     // required; e.g. 16000
	FrameMS         int     // analysis frame, default 30 ms
	PreRollMS       int     // audio kept before speech onset, default 300 ms
	StartFrames     int     // consecutive speech frames to trigger, default 3
	EndSilenceMS    int     // trailing silence to close, default 700 ms
	HangoverMS      int     // trailing silence retained, default 120 ms
	MaxUtteranceMS  int     // hard cap, default 30000 ms (§8.3 step 4)
	MinRMS          float64 // absolute activation floor, default 0.010
	ActivationRatio float64 // speech threshold = floor × ratio, default 2.5
	MaxZCR          float64 // above this zero-crossing rate = hiss, default 0.35
}

func (c Config) withDefaults() Config {
	if c.FrameMS <= 0 {
		c.FrameMS = 30
	}
	if c.PreRollMS <= 0 {
		c.PreRollMS = 300
	}
	if c.StartFrames <= 0 {
		c.StartFrames = 3
	}
	if c.EndSilenceMS <= 0 {
		c.EndSilenceMS = 700
	}
	if c.HangoverMS <= 0 {
		c.HangoverMS = 120
	}
	if c.MaxUtteranceMS <= 0 {
		c.MaxUtteranceMS = 30000
	}
	if c.MinRMS <= 0 {
		c.MinRMS = 0.010
	}
	if c.ActivationRatio <= 0 {
		c.ActivationRatio = 2.5
	}
	if c.MaxZCR <= 0 {
		c.MaxZCR = 0.35
	}
	return c
}

// EventKind identifies detector events.
type EventKind int

const (
	// EventLevel reports the RMS level of each analysis frame (UI meter).
	EventLevel EventKind = iota + 1
	// EventSpeechStart fires when an utterance begins.
	EventSpeechStart
	// EventUtterance delivers a complete trimmed utterance.
	EventUtterance
)

// Event is one detector output.
type Event struct {
	Kind      EventKind
	RMS       float64   // EventLevel
	Utterance []float32 // EventUtterance
	Truncated bool      // utterance hit MaxUtteranceMS
}

// Detector segments speech from a stream of mono float32 samples.
type Detector struct {
	cfg       Config
	frameLen  int
	buf       []float32
	preRoll   []float32 // ring of recent audio while idle
	preMax    int
	utter     []float32
	inSpeech  bool
	speechRun int
	silRun    int
	noise     []float64 // recent non-speech frame RMS
	noiseIdx  int
	noiseFull bool
	maxLen    int
	silKeep   int
}

// NewDetector builds a detector; SampleRate must be positive.
func NewDetector(cfg Config) *Detector {
	cfg = cfg.withDefaults()
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 16000
	}
	frameLen := cfg.SampleRate * cfg.FrameMS / 1000
	return &Detector{
		cfg:      cfg,
		frameLen: frameLen,
		preMax:   cfg.SampleRate * cfg.PreRollMS / 1000,
		noise:    make([]float64, 64),
		maxLen:   cfg.SampleRate * cfg.MaxUtteranceMS / 1000,
		silKeep:  cfg.SampleRate * cfg.HangoverMS / 1000,
	}
}

// Process consumes samples and returns any events they produce.
func (d *Detector) Process(samples []float32) []Event {
	var events []Event
	d.buf = append(d.buf, samples...)
	for len(d.buf) >= d.frameLen {
		frame := d.buf[:d.frameLen]
		d.buf = d.buf[d.frameLen:]
		events = append(events, d.processFrame(frame)...)
	}
	return events
}

// Flush force-ends any in-progress utterance (push-to-talk release) and
// returns the final events. The detector is ready for reuse afterwards.
func (d *Detector) Flush() []Event {
	var events []Event
	if len(d.buf) > 0 && d.inSpeech {
		d.utter = append(d.utter, d.buf...)
	}
	d.buf = nil
	if d.inSpeech && len(d.utter) > 0 {
		events = append(events, Event{Kind: EventUtterance, Utterance: d.trimTail(d.utter)})
	}
	d.reset()
	return events
}

func (d *Detector) reset() {
	d.inSpeech = false
	d.utter = nil
	d.preRoll = nil
	d.speechRun = 0
	d.silRun = 0
}

func (d *Detector) processFrame(frame []float32) []Event {
	rms := frameRMS(frame)
	zcr := frameZCR(frame)
	speech := rms >= d.threshold() && zcr <= d.cfg.MaxZCR

	events := []Event{{Kind: EventLevel, RMS: rms}}

	if !speech {
		d.noise[d.noiseIdx] = rms
		d.noiseIdx = (d.noiseIdx + 1) % len(d.noise)
		if d.noiseIdx == 0 {
			d.noiseFull = true
		}
	}

	if !d.inSpeech {
		d.preRoll = append(d.preRoll, frame...)
		if over := len(d.preRoll) - d.preMax; over > 0 {
			d.preRoll = d.preRoll[over:]
		}
		if speech {
			d.speechRun++
			if d.speechRun >= d.cfg.StartFrames {
				d.inSpeech = true
				d.utter = append([]float32{}, d.preRoll...)
				d.preRoll = nil
				d.silRun = 0
				events = append(events, Event{Kind: EventSpeechStart})
			}
		} else {
			d.speechRun = 0
		}
		return events
	}

	d.utter = append(d.utter, frame...)
	if speech {
		d.silRun = 0
	} else {
		d.silRun++
	}

	if d.silRun*d.cfg.FrameMS >= d.cfg.EndSilenceMS {
		events = append(events, Event{Kind: EventUtterance, Utterance: d.trimTail(d.utter)})
		d.reset()
		return events
	}
	if len(d.utter) >= d.maxLen {
		events = append(events, Event{
			Kind: EventUtterance, Utterance: d.trimTail(d.utter), Truncated: true,
		})
		d.reset()
	}
	return events
}

// trimTail drops trailing silence beyond the configured hangover.
func (d *Detector) trimTail(u []float32) []float32 {
	silSamples := d.silRun * d.frameLen
	if silSamples <= d.silKeep {
		return u
	}
	cut := silSamples - d.silKeep
	if cut >= len(u) {
		return u
	}
	return u[:len(u)-cut]
}

func (d *Detector) threshold() float64 {
	floor := math.Inf(1)
	n := d.noiseIdx
	if d.noiseFull {
		n = len(d.noise)
	}
	for i := 0; i < n; i++ {
		if d.noise[i] < floor {
			floor = d.noise[i]
		}
	}
	if math.IsInf(floor, 1) {
		floor = 0
	}
	t := floor * d.cfg.ActivationRatio
	if t < d.cfg.MinRMS {
		t = d.cfg.MinRMS
	}
	return t
}

func frameRMS(frame []float32) float64 {
	var sum float64
	for _, s := range frame {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(frame)))
}

func frameZCR(frame []float32) float64 {
	if len(frame) < 2 {
		return 0
	}
	crossings := 0
	for i := 1; i < len(frame); i++ {
		if (frame[i-1] >= 0) != (frame[i] >= 0) {
			crossings++
		}
	}
	return float64(crossings) / float64(len(frame)-1)
}

// Trim runs the detector over a complete buffer (push-to-talk capture) and
// returns the first trimmed utterance. ok is false when no speech was found.
func Trim(pcm []float32, cfg Config) ([]float32, bool) {
	d := NewDetector(cfg)
	var utterance []float32
	found := false
	take := func(evs []Event) {
		for _, e := range evs {
			if e.Kind == EventUtterance && !found {
				utterance = e.Utterance
				found = true
			}
		}
	}
	take(d.Process(pcm))
	take(d.Flush())
	return utterance, found
}
