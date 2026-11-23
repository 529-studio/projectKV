package push_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/push"
)

func TestClient_Send_Success(t *testing.T) {
	// 1. Generate client credentials (simulating browser)
	clientPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	clientPubB64 := base64.RawURLEncoding.EncodeToString(clientPriv.PublicKey().Bytes())

	clientAuthBytes := make([]byte, 16)
	_, _ = rand.Read(clientAuthBytes)
	clientAuthB64 := base64.RawURLEncoding.EncodeToString(clientAuthBytes)

	vapidKey, err := push.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("generate vapid key: %v", err)
	}

	var receivedHeaders http.Header
	var receivedBody []byte

	// 2. Mock push server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := push.NewClient(vapidKey, "mailto:test@529studio.site", nil, push.WithHTTPClient(server.Client()))

	sub := push.Subscription{
		ID:       "test-sub-1",
		Endpoint: server.URL + "/push-endpoint",
		P256dh:   clientPubB64,
		Auth:     clientAuthB64,
	}

	payload := push.NotificationPayload{
		Title:   "CẢNH BÁO TUYẾN ĐƯỜNG",
		Body:    "Võ Văn Kiệt ngập 35 cm",
		URL:     "/roads/vo_van_kiet",
		RoadKey: "vo_van_kiet",
	}

	// 3. Dispatch push
	err = client.Send(context.Background(), sub, payload)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// 4. Verify headers
	authHeader := receivedHeaders.Get("Authorization")
	if !strings.HasPrefix(authHeader, "vapid ") {
		t.Fatalf("expected vapid auth header, got %s", authHeader)
	}
	if receivedHeaders.Get("Content-Encoding") != "aes128gcm" {
		t.Fatalf("expected aes128gcm content-encoding, got %s", receivedHeaders.Get("Content-Encoding"))
	}
	if receivedHeaders.Get("TTL") != "86400" {
		t.Fatalf("expected TTL 86400, got %s", receivedHeaders.Get("TTL"))
	}

	// 5. Decrypt body on mock server side
	decrypted, err := push.DecryptPayloadForTest(receivedBody, clientPriv, clientAuthBytes)
	if err != nil {
		t.Fatalf("failed to decrypt received push body: %v", err)
	}

	var parsedPayload push.NotificationPayload
	if err := json.Unmarshal(decrypted, &parsedPayload); err != nil {
		t.Fatalf("failed to parse decrypted JSON: %v", err)
	}

	if parsedPayload.Title != payload.Title || parsedPayload.Body != payload.Body {
		t.Fatalf("payload mismatch: got %+v, want %+v", parsedPayload, payload)
	}
}

func TestClient_Send_PrunesExpiredSubscription(t *testing.T) {
	db := openTestDB(t)
	s := push.NewStore(db)

	clientPriv, _ := ecdh.P256().GenerateKey(rand.Reader)
	clientPubB64 := base64.RawURLEncoding.EncodeToString(clientPriv.PublicKey().Bytes())
	authB64 := base64.RawURLEncoding.EncodeToString([]byte("1234567890123456"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone) // 410 Gone
	}))
	defer server.Close()

	vapidKey, _ := push.GenerateVAPIDKeys()
	client := push.NewClient(vapidKey, "mailto:test@529studio.site", s, push.WithHTTPClient(server.Client()))

	sub := push.Subscription{
		Endpoint: server.URL + "/expired-token",
		P256dh:   clientPubB64,
		Auth:     authB64,
	}
	id, err := s.Save(context.Background(), sub)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	sub.ID = id

	err = client.Send(context.Background(), sub, push.NotificationPayload{Title: "Test"})
	if err == nil {
		t.Fatalf("expected error on 410 Gone, got nil")
	}

	// Verify subscription was automatically deleted from store
	_, found, _ := s.GetByID(context.Background(), id)
	if found {
		t.Fatalf("expected expired subscription to be deleted from store")
	}
}

func TestValidateEndpoint_SecurityRules(t *testing.T) {
	// 1. Valid endpoints
	validCases := []string{
		"https://fcm.googleapis.com/fcm/send/token123",
		"https://web.push.apple.com/QN89123",
		"https://updates.push.services.mozilla.com/wpush/v2/abc",
		"http://127.0.0.1:8080/test",
		"http://localhost:3000/test",
	}
	for _, tc := range validCases {
		if _, err := push.ValidateEndpoint(tc); err != nil {
			t.Errorf("expected %s to be valid, got error: %v", tc, err)
		}
	}

	// 2. Blocked SSRF & invalid endpoints
	invalidCases := []string{
		"",                        // empty
		"ftp://example.com/push",  // wrong scheme
		"http://example.com/push", // unencrypted HTTP non-loopback
		"http://169.254.169.254/latest/meta-data",   // cloud metadata
		"https://169.254.169.254/secret",            // cloud metadata over HTTPS
		"https://10.0.0.1/push",                     // private RFC 1918
		"https://192.168.1.1/push",                  // private RFC 1918
		"https://172.16.0.1/push",                   // private RFC 1918
		"https://metadata.google.internal/compute",  // GCP metadata
		"https://user:pass@fcm.googleapis.com/push", // userinfo credentials
	}
	for _, tc := range invalidCases {
		if _, err := push.ValidateEndpoint(tc); err == nil {
			t.Errorf("expected %s to be blocked by ValidateEndpoint, but it passed", tc)
		}
	}
}
