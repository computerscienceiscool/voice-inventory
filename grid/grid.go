// Package grid implements the Phase-B PromiseGrid message format (spec
// §11): observations encoded as CBOR messages whose protocol is referenced
// by identifier — not embedded in the payload — plus capability tokens
// (CWT via COSE_Sign1, ECDSA P-256) carrying device identity and operator
// authority with each record.
//
// The transport to an actual grid agent plugs in behind the syncer.Syncer
// interface once the agent protocol lands; this package owns the wire
// format and token crypto so that seam stays thin.
package grid

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
	cose "github.com/veraison/go-cose"

	"github.com/computerscienceiscool/voice-inventory/observation"
)

// ObservationProtocol identifies the observation message protocol by
// reference (§11: "protocol referenced by piece, not embedded").
const ObservationProtocol = "voice-inventory/observation/v1"

// Message is the CBOR grid envelope. Integer keys keep it compact.
type Message struct {
	Protocol string          `cbor:"1,keyasint"`
	Payload  cbor.RawMessage `cbor:"2,keyasint"`
	Token    []byte          `cbor:"3,keyasint,omitempty"` // COSE_Sign1 CWT
}

// CWT claim keys (RFC 8392 §3.1; the payload digest uses a Private Use
// key, RFC 8392 §9.1).
const (
	claimIss        int64 = 1
	claimSub        int64 = 2
	claimExp        int64 = 4
	claimIat        int64 = 6
	claimCti        int64 = 7
	claimPayloadSHA int64 = -65537
)

// Claims are the capability-token claims attached to a record: the device
// is the issuer, the operator the subject, the observation id the token
// identifier, and PayloadSHA256 binds the token to the exact record bytes
// so payload tampering is detectable.
type Claims struct {
	DeviceID      string
	OperatorID    string
	IssuedAt      time.Time
	Expiry        time.Time
	ObservationID string
	PayloadSHA256 []byte
}

// GenerateKey creates a device key pair (ECDSA P-256, the ES256 curve).
func GenerateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// MarshalPrivateKeyPEM serializes a device key for secure storage.
func MarshalPrivateKeyPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}

// ParsePrivateKeyPEM loads a device key.
func ParsePrivateKeyPEM(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, errors.New("grid: no EC private key PEM block")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

// Signer signs capability tokens with the device key.
type Signer struct {
	key    *ecdsa.PrivateKey
	signer cose.Signer
}

// NewSigner wraps a device key.
func NewSigner(key *ecdsa.PrivateKey) (*Signer, error) {
	cs, err := cose.NewSigner(cose.AlgorithmES256, key)
	if err != nil {
		return nil, fmt.Errorf("grid: cose signer: %w", err)
	}
	return &Signer{key: key, signer: cs}, nil
}

// Public returns the verification key.
func (s *Signer) Public() *ecdsa.PublicKey { return &s.key.PublicKey }

// SignClaims produces a COSE_Sign1 CWT for the claims.
func (s *Signer) SignClaims(c Claims) ([]byte, error) {
	m := map[int64]any{
		claimIss: c.DeviceID,
		claimSub: c.OperatorID,
		claimIat: c.IssuedAt.Unix(),
		claimCti: []byte(c.ObservationID),
	}
	if !c.Expiry.IsZero() {
		m[claimExp] = c.Expiry.Unix()
	}
	if len(c.PayloadSHA256) > 0 {
		m[claimPayloadSHA] = c.PayloadSHA256
	}
	payload, err := cbor.Marshal(m)
	if err != nil {
		return nil, err
	}
	msg := cose.NewSign1Message()
	msg.Headers.Protected.SetAlgorithm(cose.AlgorithmES256)
	msg.Payload = payload
	if err := msg.Sign(rand.Reader, nil, s.signer); err != nil {
		return nil, fmt.Errorf("grid: sign token: %w", err)
	}
	return msg.MarshalCBOR()
}

// Verifier checks capability tokens against a device public key.
type Verifier struct {
	verifier cose.Verifier
}

// NewVerifier wraps a device public key.
func NewVerifier(pub *ecdsa.PublicKey) (*Verifier, error) {
	cv, err := cose.NewVerifier(cose.AlgorithmES256, pub)
	if err != nil {
		return nil, fmt.Errorf("grid: cose verifier: %w", err)
	}
	return &Verifier{verifier: cv}, nil
}

// VerifyToken checks the signature and expiry, returning the claims.
func (v *Verifier) VerifyToken(token []byte, now time.Time) (Claims, error) {
	var msg cose.Sign1Message
	if err := msg.UnmarshalCBOR(token); err != nil {
		return Claims{}, fmt.Errorf("grid: token cbor: %w", err)
	}
	if err := msg.Verify(nil, v.verifier); err != nil {
		return Claims{}, fmt.Errorf("grid: token signature: %w", err)
	}
	var raw map[int64]cbor.RawMessage
	if err := cbor.Unmarshal(msg.Payload, &raw); err != nil {
		return Claims{}, fmt.Errorf("grid: token claims: %w", err)
	}
	var c Claims
	if err := decodeClaim(raw, claimIss, &c.DeviceID); err != nil {
		return Claims{}, err
	}
	if err := decodeClaim(raw, claimSub, &c.OperatorID); err != nil {
		return Claims{}, err
	}
	var iat int64
	if err := decodeClaim(raw, claimIat, &iat); err != nil {
		return Claims{}, err
	}
	c.IssuedAt = time.Unix(iat, 0).UTC()
	var cti []byte
	if err := decodeClaim(raw, claimCti, &cti); err != nil {
		return Claims{}, err
	}
	c.ObservationID = string(cti)
	if shaRaw, ok := raw[claimPayloadSHA]; ok {
		if err := cbor.Unmarshal(shaRaw, &c.PayloadSHA256); err != nil {
			return Claims{}, fmt.Errorf("grid: token payload digest: %w", err)
		}
	}
	if expRaw, ok := raw[claimExp]; ok {
		var exp int64
		if err := cbor.Unmarshal(expRaw, &exp); err != nil {
			return Claims{}, fmt.Errorf("grid: token exp: %w", err)
		}
		c.Expiry = time.Unix(exp, 0).UTC()
		if now.After(c.Expiry) {
			return c, fmt.Errorf("grid: token expired at %s", c.Expiry)
		}
	}
	return c, nil
}

func decodeClaim[T any](raw map[int64]cbor.RawMessage, key int64, out *T) error {
	r, ok := raw[key]
	if !ok {
		return fmt.Errorf("grid: token missing claim %d", key)
	}
	if err := cbor.Unmarshal(r, out); err != nil {
		return fmt.Errorf("grid: token claim %d: %w", key, err)
	}
	return nil
}

// EncodeObservation wraps an observation in a signed grid message. A nil
// signer produces an unsigned message (development only).
func EncodeObservation(o *observation.Observation, signer *Signer, tokenTTL time.Duration) ([]byte, error) {
	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("grid: %w", err)
	}
	payload, err := cbor.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("grid: encode payload: %w", err)
	}
	msg := Message{Protocol: ObservationProtocol, Payload: payload}
	if signer != nil {
		now := time.Now().UTC()
		digest := sha256.Sum256(payload)
		claims := Claims{
			DeviceID:      o.DeviceID,
			OperatorID:    o.OperatorID,
			IssuedAt:      now,
			ObservationID: o.ID,
			PayloadSHA256: digest[:],
		}
		if tokenTTL > 0 {
			claims.Expiry = now.Add(tokenTTL)
		}
		token, err := signer.SignClaims(claims)
		if err != nil {
			return nil, err
		}
		msg.Token = token
	}
	return cbor.Marshal(msg)
}

// DecodeObservation unwraps a grid message. When verifier is non-nil the
// token must be present, valid, unexpired, and must match the payload's
// device, operator, and record id.
func DecodeObservation(data []byte, verifier *Verifier, now time.Time) (*observation.Observation, *Claims, error) {
	var msg Message
	if err := cbor.Unmarshal(data, &msg); err != nil {
		return nil, nil, fmt.Errorf("grid: decode message: %w", err)
	}
	if msg.Protocol != ObservationProtocol {
		return nil, nil, fmt.Errorf("grid: unexpected protocol %q", msg.Protocol)
	}
	var o observation.Observation
	if err := cbor.Unmarshal(msg.Payload, &o); err != nil {
		return nil, nil, fmt.Errorf("grid: decode payload: %w", err)
	}
	if verifier == nil {
		return &o, nil, nil
	}
	if len(msg.Token) == 0 {
		return nil, nil, errors.New("grid: message has no capability token")
	}
	claims, err := verifier.VerifyToken(msg.Token, now)
	if err != nil {
		return nil, nil, err
	}
	if claims.ObservationID != o.ID || claims.DeviceID != o.DeviceID ||
		claims.OperatorID != o.OperatorID {
		return nil, nil, errors.New("grid: token claims do not match payload")
	}
	digest := sha256.Sum256(msg.Payload)
	if subtle.ConstantTimeCompare(claims.PayloadSHA256, digest[:]) != 1 {
		return nil, nil, errors.New("grid: payload does not match the signed digest")
	}
	return &o, &claims, nil
}
