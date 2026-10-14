package subscription

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandler_Endpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteStore(db)
	handler := NewHandler(store)

	r := gin.New()
	api := r.Group("/api")
	handler.RegisterRoutes(api)

	// 1. POST /api/subscriptions (Success)
	subReq := SubscribeRequest{
		ChannelName: "telegram",
		ChatID:      "12345678",
		TargetType:  "zone",
		TargetID:    "Quận 1",
	}
	body, _ := json.Marshal(subReq)
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	// 2. POST /api/subscriptions (Invalid target_type -> 400)
	subReqBad := SubscribeRequest{
		ChannelName: "telegram",
		ChatID:      "12345678",
		TargetType:  "invalid-type",
	}
	bodyBad, _ := json.Marshal(subReqBad)
	reqBad := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewReader(bodyBad))
	reqBad.Header.Set("Content-Type", "application/json")
	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, reqBad)

	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", wBad.Code)
	}

	// 3. GET /api/subscriptions (Success)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/subscriptions?channel_name=telegram&chat_id=12345678", nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", wGet.Code, wGet.Body.String())
	}

	var getResp struct {
		Subscriptions []Subscription `json:"subscriptions"`
	}
	if err := json.Unmarshal(wGet.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("unmarshal GET response: %v", err)
	}
	if len(getResp.Subscriptions) != 1 || getResp.Subscriptions[0].TargetID != "Quận 1" {
		t.Fatalf("expected 1 subscription for Quận 1, got %v", getResp.Subscriptions)
	}

	// 4. DELETE /api/subscriptions (Success)
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/subscriptions?channel_name=telegram&chat_id=12345678&target_type=zone&target_id=Qu%E1%BA%ADn%201", nil)
	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, reqDel)

	if wDel.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", wDel.Code, wDel.Body.String())
	}

	// Verify list is now empty
	wGet2 := httptest.NewRecorder()
	r.ServeHTTP(wGet2, reqGet)
	var getResp2 struct {
		Subscriptions []Subscription `json:"subscriptions"`
	}
	_ = json.Unmarshal(wGet2.Body.Bytes(), &getResp2)
	if len(getResp2.Subscriptions) != 0 {
		t.Fatalf("expected 0 subscriptions after DELETE, got %d", len(getResp2.Subscriptions))
	}
}
