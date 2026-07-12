package grid

import (
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/computerscienceiscool/voice-inventory/observation"
)

func cborMarshal(v any) ([]byte, error)   { return cbor.Marshal(v) }
func cborUnmarshal(d []byte, v any) error { return cbor.Unmarshal(d, v) }

func testObs(t *testing.T) *observation.Observation {
	t.Helper()
	o, err := observation.New("dev-1", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	o.RawTranscript = "twelve boxes of RJ45 in A-14"
	o.Parsed.ItemText = "RJ45 connectors"
	q := 12.0
	o.Parsed.Quantity = &q
	o.Parsed.LocationText = "A-14"
	o.Status = observation.StatusConfirmed
	return o
}

func TestSignedRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSigner(key)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(signer.Public())
	if err != nil {
		t.Fatal(err)
	}

	o := testObs(t)
	data, err := EncodeObservation(o, signer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, claims, err := DecodeObservation(data, verifier, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != o.ID || got.Parsed.ItemText != o.Parsed.ItemText || *got.Parsed.Quantity != 12 {
		t.Errorf("payload round trip wrong: %+v", got)
	}
	if claims.DeviceID != "dev-1" || claims.OperatorID != "op-1" || claims.ObservationID != o.ID {
		t.Errorf("claims wrong: %+v", claims)
	}
}

func TestTamperedPayloadRejected(t *testing.T) {
	key, _ := GenerateKey()
	signer, _ := NewSigner(key)
	verifier, _ := NewVerifier(signer.Public())

	o := testObs(t)
	data, err := EncodeObservation(o, signer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Re-encode with a different operator but the old token.
	o2 := *o
	o2.OperatorID = "intruder"
	tampered, err := EncodeObservation(&o2, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	// splice: decode tampered (unsigned) fails against verifier
	if _, _, err := DecodeObservation(tampered, verifier, time.Now()); err == nil {
		t.Error("unsigned message must fail verification")
	}
	// wrong-key verifier fails
	otherKey, _ := GenerateKey()
	otherVerifier, _ := NewVerifier(&otherKey.PublicKey)
	if _, _, err := DecodeObservation(data, otherVerifier, time.Now()); err == nil {
		t.Error("wrong key must fail verification")
	}
}

func TestExpiredToken(t *testing.T) {
	key, _ := GenerateKey()
	signer, _ := NewSigner(key)
	verifier, _ := NewVerifier(signer.Public())
	o := testObs(t)
	data, err := EncodeObservation(o, signer, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = DecodeObservation(data, verifier, time.Now().Add(2*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected expiry error, got %v", err)
	}
}

func TestClaimsMustMatchPayload(t *testing.T) {
	key, _ := GenerateKey()
	signer, _ := NewSigner(key)
	verifier, _ := NewVerifier(signer.Public())

	o := testObs(t)
	// Token for a different record id.
	token, err := signer.SignClaims(Claims{
		DeviceID: o.DeviceID, OperatorID: o.OperatorID,
		IssuedAt: time.Now(), ObservationID: "someone-else",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := EncodeObservation(o, nil, 0)
	var msg Message
	if err := cborUnmarshal(payload, &msg); err != nil {
		t.Fatal(err)
	}
	msg.Token = token
	respliced, err := cborMarshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeObservation(respliced, verifier, time.Now()); err == nil {
		t.Error("mismatched claims must be rejected")
	}
}

// The token must bind the payload bytes: swapping in a tampered payload
// under a valid token must fail.
func TestTamperedPayloadUnderValidToken(t *testing.T) {
	key, _ := GenerateKey()
	signer, _ := NewSigner(key)
	verifier, _ := NewVerifier(signer.Public())

	o := testObs(t)
	data, err := EncodeObservation(o, signer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var msg Message
	if err := cborUnmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	// Re-encode the same record with a different quantity, keep the token.
	tampered := *o
	q := 9999.0
	tampered.Parsed.Quantity = &q
	tamperedEnc, err := EncodeObservation(&tampered, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var tamperedMsg Message
	_ = cborUnmarshal(tamperedEnc, &tamperedMsg)
	tamperedMsg.Token = msg.Token // valid signature, same id/device/operator
	spliced, err := cborMarshal(tamperedMsg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeObservation(spliced, verifier, time.Now()); err == nil {
		t.Error("tampered payload under a valid token must be rejected")
	}
	// untampered original still verifies
	if _, _, err := DecodeObservation(data, verifier, time.Now()); err != nil {
		t.Errorf("original must still verify: %v", err)
	}
}

func TestUnsignedDevPath(t *testing.T) {
	o := testObs(t)
	data, err := EncodeObservation(o, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, claims, err := DecodeObservation(data, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claims != nil {
		t.Error("unsigned decode should return nil claims")
	}
	if got.ID != o.ID {
		t.Error("payload mismatch")
	}
}

func TestKeyPEMRoundTrip(t *testing.T) {
	key, _ := GenerateKey()
	pemBytes, err := MarshalPrivateKeyPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePrivateKeyPEM(pemBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(key) {
		t.Error("key round trip mismatch")
	}
	if _, err := ParsePrivateKeyPEM([]byte("garbage")); err == nil {
		t.Error("garbage PEM should fail")
	}
}

func TestWrongProtocolRejected(t *testing.T) {
	msg := Message{Protocol: "something/else", Payload: []byte{0xa0}}
	data, err := cborMarshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeObservation(data, nil, time.Now()); err == nil {
		t.Error("wrong protocol should be rejected")
	}
}
