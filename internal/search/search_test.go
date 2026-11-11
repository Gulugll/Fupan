package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTavilySearch(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Write([]byte(`{"results":[{"title":"交叉熵","url":"https://a.com","content":"衡量分布差异"}` +
			`,{"title":"b","url":"https://b.com","content":"x"}]}`))
	}))
	defer srv.Close()

	results, err := Search(context.Background(), "tavily", "k1", "交叉熵", 5, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer k1" || !strings.Contains(gotBody, `"search_depth":"basic"`) {
		t.Fatalf("请求形状错误: auth=%s body=%s", gotAuth, gotBody)
	}
	if len(results) != 2 || results[0].Title != "交叉熵" || results[0].URL != "https://a.com" {
		t.Fatalf("结果映射错误: %+v", results)
	}
}

func TestBochaSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"code":200,"data":{"webPages":{"value":[` +
			`{"name":"交叉熵","url":"https://a.com","snippet":"片段","summary":"摘 要"}]}}}`))
	}))
	defer srv.Close()

	results, err := Search(context.Background(), "bocha", "k2", "交叉熵", 3, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "交叉熵" || results[0].Content != "摘 要" {
		t.Fatalf("博查结果映射错误: %+v", results)
	}
}

func TestSearchDoesNotFollowRedirects(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalled = true
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	if _, err := Search(context.Background(), "tavily", "secret", "q", 5, redirect.URL); err == nil {
		t.Fatal("搜索重定向必须被拒绝")
	}
	if targetCalled {
		t.Fatal("搜索重定向目标不得收到请求")
	}
}

func TestSearchErrors(t *testing.T) {
	if _, err := Search(context.Background(), "tavily", "", "q", 5, ""); err == nil {
		t.Fatal("无 key 应报错")
	}
	if _, err := Search(context.Background(), "ghost", "k", "q", 5, ""); err == nil {
		t.Fatal("未知 provider 应报错")
	}
	// 非 200 状态码。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := Search(context.Background(), "tavily", "bad", "q", 5, srv.URL); err == nil {
		t.Fatal("鉴权失败应报错")
	}
}
