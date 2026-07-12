package grid

import (
	"testing"
	"time"

	"github.com/computerscienceiscool/voice-inventory/observation"
)

// FuzzDecodeObservation asserts message decoding never panics on arbitrary
// bytes, with and without signature verification.
func FuzzDecodeObservation(f *testing.F) {
	key, err := GenerateKey()
	if err != nil {
		f.Fatal(err)
	}
	signer, _ := NewSigner(key)
	verifier, _ := NewVerifier(signer.Public())

	obs, err := observation.New("dev-1", "op-1")
	if err != nil {
		f.Fatal(err)
	}
	obs.Parsed.ItemText = "RJ45 connectors"
	q := 12.0
	obs.Parsed.Quantity = &q
	obs.Status = observation.StatusConfirmed

	if data, err := EncodeObservation(obs, signer, time.Hour); err == nil {
		f.Add(data)
	}
	if data, err := EncodeObservation(obs, nil, 0); err == nil {
		f.Add(data)
	}
	f.Add([]byte{0xa3, 0x01, 0x60, 0x02, 0x40, 0x03, 0x40})
	f.Add([]byte{})

	now := time.Now()
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = DecodeObservation(data, nil, now)
		_, _, _ = DecodeObservation(data, verifier, now)
		_, _ = verifier.VerifyToken(data, now)
	})
}
