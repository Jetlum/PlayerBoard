package ingest

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var (
	ErrBadSignature = errors.New("signature verification failed")
	ErrStale        = errors.New("timestamp outside allowed window")
)

// MaxSkew is the replay window: webhooks older/newer than this are rejected.
const MaxSkew = 5 * time.Minute

// Verifier authenticates a raw webhook payload. Verification always runs BEFORE the
// body is deserialized, so malformed/hostile bodies never reach the parser.
type Verifier interface {
	Verify(timestamp string, rawBody []byte, signature string) error
}

// signingInput is the exact byte sequence both sides sign: "<timestamp>.<rawBody>".
func signingInput(timestamp string, rawBody []byte) []byte {
	buf := make([]byte, 0, len(timestamp)+1+len(rawBody))
	buf = append(buf, timestamp...)
	buf = append(buf, '.')
	buf = append(buf, rawBody...)
	return buf
}

// CheckTimestamp is the replay guard.
func CheckTimestamp(timestamp string, now time.Time) error {
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrStale
	}
	d := now.Sub(time.Unix(unix, 0))
	if d < 0 {
		d = -d
	}
	if d > MaxSkew {
		return ErrStale
	}
	return nil
}

// HMACVerifier is the primary scheme: HMAC-SHA256 over the signing input, constant-time compare.
type HMACVerifier struct {
	Secret []byte
}

func (v HMACVerifier) Verify(timestamp string, rawBody []byte, signature string) error {
	want, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, v.Secret)
	mac.Write(signingInput(timestamp, rawBody))
	if !hmac.Equal(want, mac.Sum(nil)) {
		return ErrBadSignature
	}
	return nil
}

// Sign produces the signature the sender would attach. Used by tests and the demo script.
func (v HMACVerifier) Sign(timestamp string, rawBody []byte) string {
	mac := hmac.New(sha256.New, v.Secret)
	mac.Write(signingInput(timestamp, rawBody))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// RSAVerifier is the swappable scheme: RSA-SHA256 (PKCS#1 v1.5) with the sender's public key.
type RSAVerifier struct {
	Pub *rsa.PublicKey
}

func (v RSAVerifier) Verify(timestamp string, rawBody []byte, signature string) error {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return ErrBadSignature
	}
	h := sha256.Sum256(signingInput(timestamp, rawBody))
	if err := rsa.VerifyPKCS1v15(v.Pub, crypto.SHA256, h[:], sig); err != nil {
		return ErrBadSignature
	}
	return nil
}

// ParseRSAPublicKey accepts a PEM-encoded PKIX or PKCS#1 RSA public key.
func ParseRSAPublicKey(pemBytes []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if pub, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		rp, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("not an RSA public key")
		}
		return rp, nil
	}
	return x509.ParsePKCS1PublicKey(block.Bytes)
}
