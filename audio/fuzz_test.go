package audio

import (
	"bytes"
	"testing"
)

// FuzzDecodeWAV asserts the decoder never panics or over-allocates on
// arbitrary input — errors are the expected outcome for garbage.
func FuzzDecodeWAV(f *testing.F) {
	var good bytes.Buffer
	_ = EncodeWAV16(&good, []float32{0, 0.5, -0.5, 0.25}, 16000)
	f.Add(good.Bytes())
	f.Add([]byte("RIFF"))
	f.Add([]byte("RIFF\x00\x00\x00\x00WAVEfmt "))
	truncated := append([]byte(nil), good.Bytes()[:20]...)
	f.Add(truncated)
	huge := append([]byte(nil), good.Bytes()...)
	huge[43] = 0xFF // corrupt the data-chunk size
	f.Add(huge)

	f.Fuzz(func(t *testing.T, data []byte) {
		samples, rate, err := DecodeWAV(bytes.NewReader(data))
		if err == nil {
			if rate < 0 {
				t.Fatalf("negative rate %d", rate)
			}
			if len(samples) > len(data) {
				t.Fatalf("decoded %d samples from %d bytes", len(samples), len(data))
			}
		}
	})
}
