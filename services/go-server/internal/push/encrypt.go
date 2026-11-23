package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	// DefaultRecordSize is 4096 bytes per RFC 8188.
	DefaultRecordSize = 4096
	// HeaderLength for RFC 8188 aes128gcm with P-256 public key (16 + 4 + 1 + 65 = 86 bytes).
	HeaderLength = 86
)

// EncryptPayload encrypts a plaintext message for a Web Push client per RFC 8291 and RFC 8188.
// clientPubBytes must be 65 bytes uncompressed P-256 point (0x04 || X || Y).
// clientAuthBytes must be 16 bytes.
func EncryptPayload(plaintext, clientPubBytes, clientAuthBytes []byte) ([]byte, error) {
	if len(clientPubBytes) != 65 || clientPubBytes[0] != 0x04 {
		return nil, fmt.Errorf("invalid client p256dh public key: expected 65 bytes with 0x04 prefix, got %d", len(clientPubBytes))
	}
	if len(clientAuthBytes) < 16 {
		return nil, fmt.Errorf("invalid client auth secret: expected at least 16 bytes, got %d", len(clientAuthBytes))
	}

	// 1. Generate ephemeral server ECDH keypair
	asPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral ecdh key: %w", err)
	}
	asPubBytes := asPriv.PublicKey().Bytes()

	// 2. Parse client public key
	clientPub, err := ecdh.P256().NewPublicKey(clientPubBytes)
	if err != nil {
		return nil, fmt.Errorf("parse client public key: %w", err)
	}

	// 3. Compute shared ECDH secret
	ecdhSecret, err := asPriv.ECDH(clientPub)
	if err != nil {
		return nil, fmt.Errorf("derive ecdh shared secret: %w", err)
	}

	// 4. Derive IKM using client auth as salt (RFC 8291 Section 3.2)
	// key_info = "WebPush: info\x00" || clientPubBytes || asPubBytes
	keyInfo := make([]byte, 0, len("WebPush: info\x00")+len(clientPubBytes)+len(asPubBytes))
	keyInfo = append(keyInfo, []byte("WebPush: info\x00")...)
	keyInfo = append(keyInfo, clientPubBytes...)
	keyInfo = append(keyInfo, asPubBytes...)

	prkReader := hkdf.New(sha256.New, ecdhSecret, clientAuthBytes, keyInfo)
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(prkReader, ikm); err != nil {
		return nil, fmt.Errorf("derive ikm: %w", err)
	}

	// 5. Generate 16-byte random salt
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	// 6. Derive Content Encryption Key (CEK) and Nonce (RFC 8188 Section 2.1)
	cekReader := hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: aes128gcm\x00"))
	cek := make([]byte, 16)
	if _, err := io.ReadFull(cekReader, cek); err != nil {
		return nil, fmt.Errorf("derive cek: %w", err)
	}

	nonceReader := hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: nonce\x00"))
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(nonceReader, nonce); err != nil {
		return nil, fmt.Errorf("derive nonce: %w", err)
	}

	// 7. Form record payload: plaintext || 0x02 (final record delimiter)
	record := make([]byte, len(plaintext)+1)
	copy(record, plaintext)
	record[len(plaintext)] = 0x02

	// 8. Encrypt record with AES-128-GCM
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	ciphertext := aesGCM.Seal(nil, nonce, record, nil)

	// 9. Construct RFC 8188 header (86 bytes total):
	// salt (16 bytes) || rs (4 bytes) || idlen (1 byte = 65) || asPubBytes (65 bytes)
	header := make([]byte, HeaderLength)
	copy(header[0:16], salt)
	binary.BigEndian.PutUint32(header[16:20], uint32(DefaultRecordSize))
	header[20] = byte(len(asPubBytes))
	copy(header[21:86], asPubBytes)

	// Full message = header || ciphertext
	result := make([]byte, len(header)+len(ciphertext))
	copy(result[:len(header)], header)
	copy(result[len(header):], ciphertext)

	return result, nil
}

// DecryptPayloadForTest decrypts an RFC 8291 payload using client credentials for test verification.
func DecryptPayloadForTest(encrypted []byte, clientPriv *ecdh.PrivateKey, clientAuth []byte) ([]byte, error) {
	if len(encrypted) < HeaderLength+1+16 {
		return nil, errors.New("encrypted payload too short")
	}

	salt := encrypted[0:16]
	// rs := binary.BigEndian.Uint32(encrypted[16:20])
	idlen := int(encrypted[20])
	if idlen != 65 {
		return nil, fmt.Errorf("unsupported key length: %d", idlen)
	}
	asPubBytes := encrypted[21:86]
	ciphertext := encrypted[86:]

	asPub, err := ecdh.P256().NewPublicKey(asPubBytes)
	if err != nil {
		return nil, fmt.Errorf("parse server public key: %w", err)
	}

	ecdhSecret, err := clientPriv.ECDH(asPub)
	if err != nil {
		return nil, fmt.Errorf("derive shared secret: %w", err)
	}

	clientPubBytes := clientPriv.PublicKey().Bytes()
	keyInfo := make([]byte, 0, len("WebPush: info\x00")+len(clientPubBytes)+len(asPubBytes))
	keyInfo = append(keyInfo, []byte("WebPush: info\x00")...)
	keyInfo = append(keyInfo, clientPubBytes...)
	keyInfo = append(keyInfo, asPubBytes...)

	prkReader := hkdf.New(sha256.New, ecdhSecret, clientAuth, keyInfo)
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(prkReader, ikm); err != nil {
		return nil, fmt.Errorf("derive ikm: %w", err)
	}

	cekReader := hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: aes128gcm\x00"))
	cek := make([]byte, 16)
	if _, err := io.ReadFull(cekReader, cek); err != nil {
		return nil, fmt.Errorf("derive cek: %w", err)
	}

	nonceReader := hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: nonce\x00"))
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(nonceReader, nonce); err != nil {
		return nil, fmt.Errorf("derive nonce: %w", err)
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	record, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm decrypt: %w", err)
	}

	// Remove final delimiter byte (0x02)
	if len(record) == 0 || record[len(record)-1] != 0x02 {
		return nil, errors.New("invalid record padding delimiter")
	}

	return record[:len(record)-1], nil
}
