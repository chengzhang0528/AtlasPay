package security

import (
	"bytes"
	"testing"
	"time"
)

func TestEncryptorBindsCiphertextToAAD(t *testing.T) {
	enc, err := NewEncryptor(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := enc.Seal([]byte("private-value"), "merchant:a")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := enc.Open(sealed, "merchant:a")
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "private-value" {
		t.Fatalf("unexpected plaintext %q", plain)
	}
	if _, err := enc.Open(sealed, "merchant:b"); err == nil {
		t.Fatal("AAD mismatch must fail")
	}
}

func TestWebhookSignatureIsDeterministic(t *testing.T) {
	ts := time.Unix(1720000000, 0).UTC()
	first := WebhookSignature("secret", ts, []byte(`{"ok":true}`))
	second := WebhookSignature("secret", ts, []byte(`{"ok":true}`))
	if first != second {
		t.Fatal("signature must be deterministic")
	}
	if first == WebhookSignature("other", ts, []byte(`{"ok":true}`)) {
		t.Fatal("different secret must change signature")
	}
}

func TestSecretsAndDigests(t *testing.T) {
	secret, err := NewSecret("mk")
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) < 40 {
		t.Fatalf("secret too short: %d", len(secret))
	}
	if Digest(secret) == Digest(secret+"x") {
		t.Fatal("digest collision in test")
	}
	if !ConstantTimeEqual(secret, secret) || ConstantTimeEqual(secret, secret+"x") {
		t.Fatal("constant-time equality returned wrong result")
	}
}
