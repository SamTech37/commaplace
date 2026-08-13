package main

import (
	"bytes"
	"testing"
)

// Render's generateValue emits a base64-ish string, not hex — it must not error.
func TestLoadOrCreateSecretNonHexEnv(t *testing.T) {
	const v = "Gk3xQz-not_hex"
	got, err := loadOrCreateSecret(v)
	if err != nil {
		t.Fatalf("non-hex env value: %v", err)
	}
	if !bytes.Equal(got, []byte(v)) {
		t.Fatalf("got %q, want raw bytes of %q", got, v)
	}
}

func TestLoadOrCreateSecretHexEnv(t *testing.T) {
	got, err := loadOrCreateSecret("00ff")
	if err != nil {
		t.Fatalf("hex env value: %v", err)
	}
	if !bytes.Equal(got, []byte{0x00, 0xff}) {
		t.Fatalf("got %v, want [0 255]", got)
	}
}
