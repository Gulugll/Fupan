package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/Gulugll/fupan/internal/config"
)

func TestHealthEndpoint(t *testing.T) {
	t.Setenv("FUPAN_VECTOR_DISABLED", "1")
	cfg := &config.Config{Dev: true, DataDir: t.TempDir()}
	app, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.db.Close()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerRoutes(r, app)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("health 返回码 = %d, 期望 200", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("health 响应不是合法 JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("health status = %q, 期望 ok", body["status"])
	}
}
