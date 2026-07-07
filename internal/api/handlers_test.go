package api

import (
	"cacti-rrd-api/internal/config"
	"cacti-rrd-api/internal/querysign"
	"cacti-rrd-api/internal/rrd"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func setupTestServer(cfg *config.Config) (http.Handler, *rrd.MockClient) {
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 5 * time.Minute
	}
	mockClient := rrd.NewMockClient(cfg.RRDDir)
	cache := rrd.NewMetricsCache(mockClient, cfg.RefreshInterval)
	
	// Start cache and give it a small moment to initialize the metrics
	cache.Start(context.Background())
	time.Sleep(50 * time.Millisecond)
	
	handler := NewAPIHandler(mockClient, cache, nil)
	router := SetupRouter(cfg, handler, nil)
	return router, mockClient
}

func TestPingHandler(t *testing.T) {
	cfg := &config.Config{}
	router, _ := setupTestServer(cfg)

	req := httptest.NewRequest("GET", "/api/v1/ping", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp != "pong" {
		t.Errorf("Expected 'pong', got %q", resp)
	}
}

func TestListMetricsHandlerUnauthenticated(t *testing.T) {
	cfg := &config.Config{}
	router, _ := setupTestServer(cfg)

	req := httptest.NewRequest("GET", "/api/v1/list_metrics", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var metrics []string
	if err := json.Unmarshal(w.Body.Bytes(), &metrics); err != nil {
		t.Fatalf("Failed to decode metrics: %v", err)
	}

	// MockClient has 4 RRD files, one with 2 DS, so total 5 metrics
	if len(metrics) != 5 {
		t.Errorf("Expected 5 metrics, got %d", len(metrics))
	}
}

func TestListMetricsHandlerDetailed(t *testing.T) {
	cfg := &config.Config{}
	router, _ := setupTestServer(cfg)

	req := httptest.NewRequest("GET", "/api/v1/list_metrics?detail=true", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var details []MetricDetail
	if err := json.Unmarshal(w.Body.Bytes(), &details); err != nil {
		t.Fatalf("Failed to decode detailed metrics: %v. Body: %s", err, w.Body.String())
	}

	if len(details) != 5 {
		t.Errorf("Expected 5 detailed metrics, got %d", len(details))
	}

	for _, det := range details {
		if det.Metric == "" || det.File == "" || det.Ds == "" || det.Title == "" {
			t.Errorf("MetricDetail fields should not be empty: %+v", det)
		}
	}
}

func TestBasicAuthMiddleware(t *testing.T) {
	cfg := &config.Config{
		BasicAuthUser: "cacti_admin",
		BasicAuthPass: "super_secret_password",
	}
	router, _ := setupTestServer(cfg)

	// 1. Request without Basic Auth should fail
	req := httptest.NewRequest("GET", "/api/v1/list_metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 Unauthorized for missing credentials, got %d", w.Code)
	}

	// 2. Request with invalid credentials should fail
	req = httptest.NewRequest("GET", "/api/v1/list_metrics", nil)
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:wrongpass"))
	req.Header.Set("Authorization", authHeader)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 Unauthorized for invalid credentials, got %d", w.Code)
	}

	// 3. Request with valid credentials should pass
	req = httptest.NewRequest("GET", "/api/v1/list_metrics", nil)
	authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte("cacti_admin:super_secret_password"))
	req.Header.Set("Authorization", authHeader)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 OK for valid credentials, got %d", w.Code)
	}
}

func TestHMACSignatureMiddleware(t *testing.T) {
	secret := "my_query_signing_secret"
	cfg := &config.Config{
		SignedQuerySecret: secret,
	}
	router, _ := setupTestServer(cfg)

	path := "/api/v1/list_metrics"
	expiry := time.Now().Add(5 * time.Minute).Unix()
	queryParams := "glob=*&x=" + strconv.FormatInt(expiry, 10)

	// 1. Valid signature should pass
	signedQuery := querysign.SignQuery([]byte(secret), []byte(path), []byte(queryParams))
	req := httptest.NewRequest("GET", path+"?"+string(signedQuery), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 OK for valid signature, got %d. Body: %s", w.Code, w.Body.String())
	}

	// 2. Invalid signature should fail
	invalidQuery := string(signedQuery[:len(signedQuery)-5]) + "aaaaa"
	req = httptest.NewRequest("GET", path+"?"+invalidQuery, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 Unauthorized for invalid signature, got %d", w.Code)
	}

	// 3. Expired query should fail
	pastExpiry := time.Now().Add(-5 * time.Minute).Unix()
	expiredParams := "glob=*&x=" + strconv.FormatInt(pastExpiry, 10)
	expiredSignedQuery := querysign.SignQuery([]byte(secret), []byte(path), []byte(expiredParams))
	
	req = httptest.NewRequest("GET", path+"?"+string(expiredSignedQuery), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 Unauthorized for expired query, got %d", w.Code)
	}
}

func TestXportHandler(t *testing.T) {
	cfg := &config.Config{}
	router, _ := setupTestServer(cfg)

	// Build a valid xport request
	xportSpec := "DEF:val=localhost_mem_buffers_3.rrd:mem_buffers:AVERAGE XPORT:val:mem_buffers"
	req := httptest.NewRequest("GET", "/api/v1/xport?start=-1h&xport="+strings.ReplaceAll(xportSpec, " ", "%20"), nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %d. Error: %s", w.Code, w.Body.String())
	}

	// Verify that the body is valid JSON containing meta and data
	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	meta, ok := result["meta"].(map[string]interface{})
	if !ok {
		t.Errorf("Expected result to contain meta dictionary")
	} else {
		legend, ok := meta["legend"].([]interface{})
		if !ok || len(legend) != 1 || legend[0] != "mem_buffers" {
			t.Errorf("Expected legend to contain ['mem_buffers'], got %v", legend)
		}
	}
}

func TestGraphHandler(t *testing.T) {
	cfg := &config.Config{}
	router, _ := setupTestServer(cfg)

	// Build a valid graph request
	graphSpec := "DEF:val=localhost_mem_buffers_3.rrd:mem_buffers:AVERAGE AREA:val#38a169:mem_buffers"
	req := httptest.NewRequest("GET", "/api/v1/graph?start=-1h&imgformat=SVG&graph="+strings.ReplaceAll(graphSpec, " ", "%20"), nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %d. Error: %s", w.Code, w.Body.String())
	}

	if w.Header().Get("Content-Type") != "image/svg+xml" {
		t.Errorf("Expected content type 'image/svg+xml', got %q", w.Header().Get("Content-Type"))
	}

	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, "<svg") || !strings.Contains(bodyStr, "</svg>") {
		t.Errorf("Expected body to contain SVG tags")
	}
}

func TestGraphsListAndRenderByID(t *testing.T) {
	cfg := &config.Config{}
	router, _ := setupTestServer(cfg)

	// 1. Test List Graphs
	req := httptest.NewRequest("GET", "/api/v1/graphs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %d. Error: %s", w.Code, w.Body.String())
	}

	var graphs []rrd.GraphDefinition
	if err := json.Unmarshal(w.Body.Bytes(), &graphs); err != nil {
		t.Fatalf("Failed to decode graphs list: %v", err)
	}

	// Should fall back to 3 mock graphs
	if len(graphs) != 3 {
		t.Errorf("Expected 3 fallback graphs, got %d", len(graphs))
	}

	// 2. Test Render Graph by ID
	req = httptest.NewRequest("GET", "/api/v1/graphs/render?id=1&imgformat=SVG", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %d. Error: %s", w.Code, w.Body.String())
	}

	if w.Header().Get("Content-Type") != "image/svg+xml" {
		t.Errorf("Expected content type 'image/svg+xml', got %q", w.Header().Get("Content-Type"))
	}

	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, "<svg") || !strings.Contains(bodyStr, "</svg>") {
		t.Errorf("Expected body to contain SVG tags")
	}

	// 3. Test Render non-existent graph
	req = httptest.NewRequest("GET", "/api/v1/graphs/render?id=99&imgformat=SVG", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404 Not Found for invalid graph ID, got %d", w.Code)
	}
}
