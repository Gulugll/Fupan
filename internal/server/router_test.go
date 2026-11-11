package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/config"
	"github.com/gin-gonic/gin"
)

func TestServeHTTPGracefulShutdownWaitsForHandler(t *testing.T) {
	handlerStarted := make(chan struct{})
	unblockHandler := make(chan struct{})
	var unblockOnce sync.Once
	releaseHandler := func() {
		unblockOnce.Do(func() { close(unblockHandler) })
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		<-unblockHandler
		w.WriteHeader(http.StatusNoContent)
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		releaseHandler()
		_ = srv.Close()
		_ = listener.Close()
	})

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serveHTTP(ctx, srv, listener)
	}()
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String() + "/slow")
		if err == nil {
			_ = response.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP handler 未开始处理请求")
	}

	cancel()
	select {
	case err := <-serveDone:
		t.Fatalf("在途 handler 未放行前 serveHTTP 不得退出: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	releaseHandler()

	select {
	case err := <-serveDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("优雅关闭返回意外错误: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler 放行后 serveHTTP 仍未退出")
	}
	select {
	case err := <-requestDone:
		if err != nil {
			t.Fatalf("在途请求应正常完成: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler 放行后客户端请求仍未完成")
	}
}

func TestServeHTTPListenerErrorWaitsForHandler(t *testing.T) {
	handlerStarted := make(chan struct{})
	unblockHandler := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(handlerStarted)
		<-unblockHandler
		w.WriteHeader(http.StatusNoContent)
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- serveHTTP(context.Background(), srv, listener) }()
	requestDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/slow")
		if err == nil {
			response.Body.Close()
		}
		requestDone <- err
	}()
	<-handlerStarted
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serveDone:
		t.Fatalf("Serve 异常时在途 handler 未放行前不得返回: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(unblockHandler)
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handler 放行后 serveHTTP 未退出")
	}
	if err := <-requestDone; err != nil {
		t.Fatalf("在途请求应正常完成: %v", err)
	}
}

func TestHealthEndpoint(t *testing.T) {
	t.Setenv("FUPAN_VECTOR_DISABLED", "1")
	cfg := &config.Config{Dev: true, DataDir: t.TempDir(), NoAuth: true}
	app, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerRoutes(r, app)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newTestRequest(http.MethodGet, "/api/health", nil))

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
