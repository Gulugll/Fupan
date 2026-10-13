package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/Gulugll/fupan/internal/config"
	"github.com/Gulugll/fupan/internal/vector"
)

// testApp 组装：内存级 SQLite（临时目录）+ Noop 向量库（不连 Milvus）。
func testApp(t *testing.T) (*App, *gin.Engine) {
	t.Setenv("FUPAN_VECTOR_DISABLED", "1")
	cfg := &config.Config{Dev: true, DataDir: t.TempDir(), Retention: 0.9, DailyNewLimit: 20, EmbedDim: 2}
	app, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app.vectors = vector.NoopStore{}
	app.importSvc.SetVectorStore(app.vectors)
	t.Cleanup(func() { app.db.Close() })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerRoutes(r, app)
	return app, r
}

// multipartUpload 构造带文件的 multipart 请求。
func multipartUpload(t *testing.T, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	h.Set("Content-Type", "application/octet-stream")
	fw, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte(content))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/x", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestLibraryAndDocumentAPI(t *testing.T) {
	_, r := testApp(t)

	// 建库。
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"name": "考研库", "description": "政治+数学"})
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/libraries", bytes.NewReader(body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("建库失败: %d %s", w.Code, w.Body.String())
	}
	var lib map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &lib)
	libID, _ := lib["id"].(string)
	if libID == "" {
		t.Fatal("未返回库 id")
	}

	// 列表。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/libraries", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "考研库") {
		t.Fatalf("列表失败: %d %s", w.Code, w.Body.String())
	}

	// 上传 MD（每个小节 >100 token，避免触发合并规则）。
	md := "# 笔记\n\n" +
		"## 第一章\n\n" + strings.Repeat("这是第一章的内容，讲了机器学习的基础概念与常见术语，包括样本特征标签模型假设空间与经验风险等。", 4) + "\n\n" +
		"## 第二章\n\n" + strings.Repeat("第二章讲进阶内容，深入讨论了泛化误差的界定、正则化技术以及交叉验证在模型选择中的实际应用方法。", 4) + "\n"
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/libraries/"+libID+"/documents", nil)
	// multipart 构造。
	req = multipartUpload(t, "sample.md", md)
	req.URL.Path = "/api/libraries/" + libID + "/documents"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("上传失败: %d %s", w.Code, w.Body.String())
	}
	var doc map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &doc)
	if doc["chunk_count"].(float64) < 2 {
		t.Fatalf("chunk_count = %v", doc["chunk_count"])
	}

	// 文档详情含 chunks。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/documents/"+doc["id"].(string), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("详情失败: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "第一章") || !strings.Contains(w.Body.String(), "title_path") {
		t.Fatalf("详情缺 chunk 或 metadata: %s", w.Body.String())
	}

	// 删除文档。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/documents/"+doc["id"].(string), nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("删除失败: %d", w.Code)
	}
}

func TestUploadRejectsUnsupportedFormat(t *testing.T) {
	_, r := testApp(t)
	libID := createLib(t, r)

	w := httptest.NewRecorder()
	req := multipartUpload(t, "paper.pdf", "%PDF-1.4")
	req.URL.Path = "/api/libraries/" + libID + "/documents"
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "仅支持") {
		t.Fatalf("pdf 应被拒绝: %d %s", w.Code, w.Body.String())
	}
}

func createLib(t *testing.T, r *gin.Engine) string {
	t.Helper()
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"name": "测试库"})
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/libraries", bytes.NewReader(body)))
	var lib map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &lib)
	return lib["id"].(string)
}
