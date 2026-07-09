package ingest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestHMACVerifier(t *testing.T) {
	v := HMACVerifier{Secret: []byte("shared-secret")}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"athlete_id":"a","metric":"appearances","value":20}`)
	sig := v.Sign(ts, body)

	if err := v.Verify(ts, body, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	// Tampered body must fail.
	if err := v.Verify(ts, []byte(`{"athlete_id":"a","metric":"appearances","value":21}`), sig); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered body: got %v, want ErrBadSignature", err)
	}
	// Wrong secret must fail.
	other := HMACVerifier{Secret: []byte("attacker")}
	if err := v.Verify(ts, body, other.Sign(ts, body)); !errors.Is(err, ErrBadSignature) {
		t.Errorf("wrong secret: got %v, want ErrBadSignature", err)
	}
	// Non-base64 signature must fail, not panic.
	if err := v.Verify(ts, body, "!!!not-base64!!!"); !errors.Is(err, ErrBadSignature) {
		t.Errorf("garbage signature: got %v, want ErrBadSignature", err)
	}
}

func TestCheckTimestamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	ok := strconv.FormatInt(now.Unix(), 10)
	if err := CheckTimestamp(ok, now); err != nil {
		t.Errorf("fresh timestamp rejected: %v", err)
	}
	stale := strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10)
	if err := CheckTimestamp(stale, now); !errors.Is(err, ErrStale) {
		t.Errorf("stale timestamp: got %v, want ErrStale", err)
	}
	future := strconv.FormatInt(now.Add(6*time.Minute).Unix(), 10)
	if err := CheckTimestamp(future, now); !errors.Is(err, ErrStale) {
		t.Errorf("future timestamp: got %v, want ErrStale", err)
	}
	if err := CheckTimestamp("not-a-number", now); !errors.Is(err, ErrStale) {
		t.Errorf("bad timestamp: got %v, want ErrStale", err)
	}
}

func TestRSAVerifier(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	v := RSAVerifier{Pub: &key.PublicKey}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"athlete_id":"a","metric":"appearances","value":20}`)

	h := sha256.Sum256(signingInput(ts, body))
	raw, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := base64.StdEncoding.EncodeToString(raw)

	if err := v.Verify(ts, body, sig); err != nil {
		t.Fatalf("valid RSA signature rejected: %v", err)
	}
	if err := v.Verify(ts, append(body, '!'), sig); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered body: got %v, want ErrBadSignature", err)
	}
}
