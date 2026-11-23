package push_test

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/push"
	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupTestRouter(h *push.Handler) *gin.Engine {
	r := gin.New()
	api := r.Group("/api")
	h.RegisterRoutes(api)
	return r
}

func generateValidBrowserKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	curve := ecdh.P256()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate browser key: %v", err)
	}
	pubBytes := priv.PublicKey().Bytes()
	p256dh = base64.RawURLEncoding.EncodeToString(pubBytes)

	authBytes := make([]byte, 16)
	if _, err := rand.Read(authBytes); err != nil {
		t.Fatalf("failed to generate auth secret: %v", err)
	}
	auth = base64.RawURLEncoding.EncodeToString(authBytes)
	return p256dh, auth
}

func TestHandler_GetVAPIDKey(t *testing.T) {
	vapidKey, err := push.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("failed to generate vapid keys: %v", err)
	}

	h := push.NewHandler(vapidKey, nil, nil, nil)
	r := setupTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/push/vapid-key", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}
	if res["public_key"] != vapidKey.PublicKeyBase64() {
		t.Fatalf("expected public key %s, got %s", vapidKey.PublicKeyBase64(), res["public_key"])
	}

	// Test unconfigured key returns 503
	hNil := push.NewHandler(nil, nil, nil, nil)
	rNil := setupTestRouter(hNil)
	wNil := httptest.NewRecorder()
	rNil.ServeHTTP(wNil, req)
	if wNil.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 for nil key, got %d", wNil.Code)
	}
}

func TestHandler_Subscribe(t *testing.T) {
	db := openTestDB(t)
	pushStore := push.NewStore(db)
	subStore := subscription.NewSQLiteStore(db)

	h := push.NewHandler(nil, pushStore, subStore, nil)
	r := setupTestRouter(h)

	p256dh, auth := generateValidBrowserKeys(t)

	subReq := push.SubscribeRequest{
		Endpoint:   "https://fcm.googleapis.com/fcm/send/test-sub-123",
		TargetType: "corridor",
		TargetID:   "dien-bien-phu",
	}
	subReq.Keys.P256dh = p256dh
	subReq.Keys.Auth = auth

	bodyBytes, _ := json.Marshal(subReq)
	req := httptest.NewRequest(http.MethodPost, "/api/push/subscribe", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res["status"] != "ok" || res["id"] == "" {
		t.Fatalf("unexpected response: %v", res)
	}

	// Verify persistence in push store
	savedSub, found, err := pushStore.GetByID(req.Context(), res["id"])
	if err != nil || !found {
		t.Fatalf("expected push subscription in store, found=%t, err=%v", found, err)
	}
	if savedSub.Endpoint != subReq.Endpoint {
		t.Errorf("expected endpoint %s, got %s", subReq.Endpoint, savedSub.Endpoint)
	}

	// Test invalid body missing keys -> 400
	badReq := httptest.NewRequest(http.MethodPost, "/api/push/subscribe", bytes.NewReader([]byte(`{"endpoint":""}`)))
	badReq.Header.Set("Content-Type", "application/json")
	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, badReq)
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty endpoint, got %d", wBad.Code)
	}

	// Test invalid target_type -> 400
	badTargetReq := push.SubscribeRequest{
		Endpoint:   "https://fcm.googleapis.com/fcm/send/test-sub-456",
		TargetType: "invalid_target_type",
	}
	badTargetReq.Keys.P256dh = p256dh
	badTargetReq.Keys.Auth = auth
	badTargetBytes, _ := json.Marshal(badTargetReq)
	reqBadTarget := httptest.NewRequest(http.MethodPost, "/api/push/subscribe", bytes.NewReader(badTargetBytes))
	reqBadTarget.Header.Set("Content-Type", "application/json")
	wBadTarget := httptest.NewRecorder()
	r.ServeHTTP(wBadTarget, reqBadTarget)
	if wBadTarget.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid target_type, got %d", wBadTarget.Code)
	}
}

func TestHandler_Unsubscribe(t *testing.T) {
	db := openTestDB(t)
	pushStore := push.NewStore(db)
	subStore := subscription.NewSQLiteStore(db)

	h := push.NewHandler(nil, pushStore, subStore, nil)
	r := setupTestRouter(h)

	p256dh, auth := generateValidBrowserKeys(t)
	endpoint := "https://fcm.googleapis.com/fcm/send/test-unsub"

	// Pre-populate
	sub := push.Subscription{
		Endpoint: endpoint,
		P256dh:   p256dh,
		Auth:     auth,
	}
	id, err := pushStore.Save(t.Context(), sub)
	if err != nil {
		t.Fatalf("failed to pre-populate push sub: %v", err)
	}
	_ = subStore.Subscribe(t.Context(), subscription.Subscription{
		ChannelName: "webpush",
		ChatID:      id,
		TargetType:  subscription.TargetAll,
	})

	// Unsubscribe
	unsubReq := push.UnsubscribeRequest{Endpoint: endpoint}
	unsubBytes, _ := json.Marshal(unsubReq)
	req := httptest.NewRequest(http.MethodPost, "/api/push/unsubscribe", bytes.NewReader(unsubBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted from push store
	_, found, err := pushStore.GetByID(t.Context(), id)
	if err != nil || found {
		t.Fatalf("expected sub to be deleted, found=%t, err=%v", found, err)
	}
}

func TestHandler_TestPush(t *testing.T) {
	vapidKey, err := push.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("failed to generate vapid keys: %v", err)
	}

	// Mock push service server
	var receivedPush bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPush = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer mockServer.Close()

	db := openTestDB(t)
	pushStore := push.NewStore(db)
	client := push.NewClient(vapidKey, "mailto:admin@529studio.site", pushStore, push.WithHTTPClient(mockServer.Client()))

	h := push.NewHandler(vapidKey, pushStore, nil, client)
	r := setupTestRouter(h)

	p256dh, auth := generateValidBrowserKeys(t)
	testReq := push.TestPushRequest{
		Endpoint: mockServer.URL + "/push-target",
	}
	testReq.Keys.P256dh = p256dh
	testReq.Keys.Auth = auth

	reqBytes, _ := json.Marshal(testReq)
	req := httptest.NewRequest(http.MethodPost, "/api/push/test", bytes.NewReader(reqBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !receivedPush {
		t.Fatalf("expected mock push server to receive test push request")
	}
}
