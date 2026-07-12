// Package audio provides the PCM utilities of the capture pipeline
// (spec §8.3): resampling to Whisper's 16 kHz mono float32 input, channel
// downmix, int16 conversion, a light high-pass filter for warehouse rumble,
// and WAV encode/decode for clip retention and desktop tooling.
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// WhisperRate is the sample rate Whisper expects.
const WhisperRate = 16000

// Resample converts samples from one rate to another with linear
// interpolation. It returns the input unchanged when rates match.
func Resample(in []float32, fromRate, toRate int) []float32 {
	if fromRate <= 0 || toRate <= 0 || len(in) == 0 || fromRate == toRate {
		return in
	}
	ratio := float64(fromRate) / float64(toRate)
	outLen := int(math.Floor(float64(len(in))/ratio))
	if outLen == 0 {
		return nil
	}
	out := make([]float32, outLen)
	for i := range out {
		pos := float64(i) * ratio
		j := int(pos)
		frac := float32(pos - float64(j))
		if j+1 < len(in) {
			out[i] = in[j]*(1-frac) + in[j+1]*frac
		} else {
			out[i] = in[j]
		}
	}
	return out
}

// MonoFromInterleaved averages interleaved channels down to mono.
func MonoFromInterleaved(in []float32, channels int) []float32 {
	if channels <= 1 {
		return in
	}
	frames := len(in) / channels
	out := make([]float32, frames)
	for i := 0; i < frames; i++ {
		var sum float32
		for c := 0; c < channels; c++ {
			sum += in[i*channels+c]
		}
		out[i] = sum / float32(channels)
	}
	return out
}

// Int16ToFloat32 converts little-endian 16-bit PCM bytes to float32 samples
// in [-1, 1).
func Int16ToFloat32(b []byte) []float32 {
	n := len(b) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		v := int16(binary.LittleEndian.Uint16(b[i*2:]))
		out[i] = float32(v) / 32768
	}
	return out
}

// Float32ToInt16 converts float32 samples to little-endian 16-bit PCM bytes,
// clamping out-of-range values.
func Float32ToInt16(samples []float32) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		v := int32(s * 32767)
		if v > 32767 {
			v = 32767
		}
		if v < -32768 {
			v = -32768
		}
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(v)))
	}
	return out
}

// HighPass applies a first-order high-pass filter (DC block / low-rumble
// cut) in place and returns the slice. cutoffHz around 100 suits warehouse
// ambience (§8.3 step 3).
func HighPass(samples []float32, sampleRate int, cutoffHz float64) []float32 {
	if len(samples) == 0 || sampleRate <= 0 || cutoffHz <= 0 {
		return samples
	}
	rc := 1 / (2 * math.Pi * cutoffHz)
	dt := 1 / float64(sampleRate)
	alpha := float32(rc / (rc + dt))
	prevIn := samples[0]
	prevOut := samples[0] * 0
	for i, s := range samples {
		out := alpha * (prevOut + s - prevIn)
		prevIn = s
		prevOut = out
		samples[i] = out
	}
	return samples
}

// RMS returns the root-mean-square level of the samples.
func RMS(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(samples)))
}

// ---------------------------------------------------------------------------
// WAV

// EncodeWAV16 writes samples as a 16-bit PCM mono WAV file.
func EncodeWAV16(w io.Writer, samples []float32, sampleRate int) error {
	if sampleRate <= 0 {
		return errors.New("audio: sample rate must be positive")
	}
	data := Float32ToInt16(samples)
	var header [44]byte
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+len(data)))
	copy(header[8:], "WAVE")
	copy(header[12:], "fmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:], 1) // mono
	binary.LittleEndian.PutUint32(header[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(data)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// DecodeWAV reads a PCM WAV file (16-bit int or 32-bit float, any channel
// count) and returns mono float32 samples plus the sample rate.
func DecodeWAV(r io.Reader) ([]float32, int, error) {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return nil, 0, fmt.Errorf("audio: read RIFF header: %w", err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return nil, 0, errors.New("audio: not a WAV file")
	}
	var (
		format     uint16
		channels   uint16
		sampleRate uint32
		bits       uint16
		haveFmt    bool
	)
	for {
		var chunk [8]byte
		if _, err := io.ReadFull(r, chunk[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, 0, errors.New("audio: no data chunk")
			}
			return nil, 0, err
		}
		id := string(chunk[0:4])
		size := binary.LittleEndian.Uint32(chunk[4:])
		switch id {
		case "fmt ":
			body := make([]byte, size)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, 0, err
			}
			if size < 16 {
				return nil, 0, errors.New("audio: fmt chunk too small")
			}
			format = binary.LittleEndian.Uint16(body[0:])
			channels = binary.LittleEndian.Uint16(body[2:])
			sampleRate = binary.LittleEndian.Uint32(body[4:])
			bits = binary.LittleEndian.Uint16(body[14:])
			haveFmt = true
		case "data":
			if !haveFmt {
				return nil, 0, errors.New("audio: data before fmt chunk")
			}
			body := make([]byte, size)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, 0, err
			}
			samples, err := decodeSamples(body, format, bits)
			if err != nil {
				return nil, 0, err
			}
			return MonoFromInterleaved(samples, int(channels)), int(sampleRate), nil
		default:
			if _, err := io.CopyN(io.Discard, r, int64(size)); err != nil {
				return nil, 0, err
			}
		}
		if size%2 == 1 { // chunks are word-aligned
			if _, err := io.CopyN(io.Discard, r, 1); err != nil && !errors.Is(err, io.EOF) {
				return nil, 0, err
			}
		}
	}
}

func decodeSamples(body []byte, format, bits uint16) ([]float32, error) {
	switch {
	case format == 1 && bits == 16:
		return Int16ToFloat32(body), nil
	case format == 3 && bits == 32:
		n := len(body) / 4
		out := make([]float32, n)
		for i := 0; i < n; i++ {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(body[i*4:]))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("audio: unsupported WAV format %d/%d-bit", format, bits)
	}
}
