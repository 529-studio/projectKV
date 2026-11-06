package subscription

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type mockTaxonomyProvider struct {
	zones       []string
	wards       []WardInfo
	chokepoints []ChokepointInfo
}

func (m *mockTaxonomyProvider) Zones() []string {
	return m.zones
}

func (m *mockTaxonomyProvider) Wards() []WardInfo {
	return m.wards
}

func (m *mockTaxonomyProvider) Chokepoints() []ChokepointInfo {
	return m.chokepoints
}

func (m *mockTaxonomyProvider) HasZone(zone string) bool {
	for _, z := range m.zones {
		if z == zone {
			return true
		}
	}
	return false
}

func (m *mockTaxonomyProvider) HasWard(ward string) bool {
	for _, w := range m.wards {
		if w.Name == ward || w.Type+" "+w.Name == ward {
			return true
		}
	}
	return false
}

func (m *mockTaxonomyProvider) HasChokepoint(id string) bool {
	for _, cp := range m.chokepoints {
		if cp.ID == id {
			return true
		}
	}
	return false
}

func TestHandler_Endpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteStore(db)
	mockTax := &mockTaxonomyProvider{
		zones: CanonicalZones,
		wards: []WardInfo{
			{Type: "Phường", Name: "Bến Nghé"},
			{Type: "Phường", Name: "Thảo Điền"},
		},
		chokepoints: []ChokepointInfo{
			{ID: "cam-vvk-01", Name: "Võ Văn Kiệt"},
		},
	}
	handler := NewHandler(store, mockTax)

	r := gin.New()
	api := r.Group("/api")
	handler.RegisterRoutes(api)

	// 1. POST /api/subscriptions (Success with alias normalization)
	subReq := SubscribeRequest{
		ChannelName: "telegram",
		ChatID:      "12345678",
		TargetType:  "zone",
		TargetID:    "Quận 7", // legacy alias
	}
	body, _ := json.Marshal(subReq)
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	// 2. POST /api/subscriptions (Invalid target_type -> 400 with valid target types)
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

	var badResp map[string]interface{}
	if err := json.Unmarshal(wBad.Body.Bytes(), &badResp); err != nil {
		t.Fatalf("failed to unmarshal bad response: %v", err)
	}
	if _, ok := badResp["valid_target_types"]; !ok {
		t.Errorf("expected valid_target_types in 400 response, got %v", badResp)
	}

	// 3. POST /api/subscriptions (Invalid zone -> 400 with valid zones)
	subReqBadZone := SubscribeRequest{
		ChannelName: "telegram",
		ChatID:      "12345678",
		TargetType:  "zone",
		TargetID:    "Sao Hỏa",
	}
	bodyBadZone, _ := json.Marshal(subReqBadZone)
	reqBadZone := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewReader(bodyBadZone))
	reqBadZone.Header.Set("Content-Type", "application/json")
	wBadZone := httptest.NewRecorder()
	r.ServeHTTP(wBadZone, reqBadZone)

	if wBadZone.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid zone, got %d", wBadZone.Code)
	}

	// 4. GET /api/subscriptions (Success - targetID normalized to canonical zone)
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
	if len(getResp.Subscriptions) != 1 || getResp.Subscriptions[0].TargetID != "Quận 7 - Nhà Bè" {
		t.Fatalf("expected 1 subscription normalized to 'Quận 7 - Nhà Bè', got %v", getResp.Subscriptions)
	}

	// 5. GET /api/subscriptions/taxonomy (Success)
	reqTax := httptest.NewRequest(http.MethodGet, "/api/subscriptions/taxonomy", nil)
	wTax := httptest.NewRecorder()
	r.ServeHTTP(wTax, reqTax)

	if wTax.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for taxonomy, got %d: %s", wTax.Code, wTax.Body.String())
	}

	var taxResp TaxonomyResponse
	if err := json.Unmarshal(wTax.Body.Bytes(), &taxResp); err != nil {
		t.Fatalf("failed to decode taxonomy response: %v", err)
	}
	if len(taxResp.TargetTypes) != 4 {
		t.Errorf("expected 4 target types, got %d", len(taxResp.TargetTypes))
	}
	if len(taxResp.Zones) != len(CanonicalZones) {
		t.Errorf("expected %d zones, got %d", len(CanonicalZones), len(taxResp.Zones))
	}

	// 6. DELETE /api/subscriptions (Success with alias normalization)
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/subscriptions?channel_name=telegram&chat_id=12345678&target_type=zone&target_id=Qu%E1%BA%ADn%207", nil)
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
