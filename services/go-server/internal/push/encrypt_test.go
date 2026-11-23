package push_test

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/push"
)

func TestEncryptPayload_RoundTrip(t *testing.T) {
	// 1. Generate client credentials (simulating browser)
	clientPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	clientPubBytes := clientPriv.PublicKey().Bytes()

	clientAuthBytes := make([]byte, 16)
	if _, err := rand.Read(clientAuthBytes); err != nil {
		t.Fatalf("generate client auth: %v", err)
	}

	plaintext := []byte("Cảnh báo ngập: Tuyến đường Võ Văn Kiệt ngập 35cm (Nguy cơ chết máy)")

	// 2. Encrypt payload
	encrypted, err := push.EncryptPayload(plaintext, clientPubBytes, clientAuthBytes)
	if err != nil {
		t.Fatalf("EncryptPayload failed: %v", err)
	}

	// Header is 86 bytes (16 salt + 4 rs + 1 idlen + 65 key)
	if len(encrypted) <= 86 {
		t.Fatalf("encrypted payload too short: %d bytes", len(encrypted))
	}

	// 3. Decrypt with client private key to prove RFC 8291 compliance
	decrypted, err := push.DecryptPayloadForTest(encrypted, clientPriv, clientAuthBytes)
	if err != nil {
		t.Fatalf("DecryptPayloadForTest failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted payload mismatch:\ngot:  %s\nwant: %s", string(decrypted), string(plaintext))
	}
}

func TestEncryptPayload_InvalidInputs(t *testing.T) {
	validAuth := make([]byte, 16)
	_, err := push.EncryptPayload([]byte("hello"), []byte("short"), validAuth)
	if err == nil {
		t.Fatalf("expected error on invalid client pub key length")
	}

	validPub := make([]byte, 65)
	validPub[0] = 0x04
	_, err = push.EncryptPayload([]byte("hello"), validPub, []byte("short"))
	if err == nil {
		t.Fatalf("expected error on invalid client auth length")
	}

	oversized := make([]byte, push.MaxPayloadSize+1)
	if _, err := push.EncryptPayload(oversized, validPub, validAuth); err == nil {
		t.Fatalf("expected error for oversized payload > %d bytes, got nil", push.MaxPayloadSize)
	}
}
