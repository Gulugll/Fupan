package server

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/repo"
)

type createLibraryReq struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

func (a *App) listLibraries(c *gin.Context) {
	libs, err := a.store.ListLibraries(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if libs == nil {
		libs = []domain.Library{}
	}
	c.JSON(http.StatusOK, libs)
}

func (a *App) createLibrary(c *gin.Context) {
	var req createLibraryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	lib := &domain.Library{
		ID:          uuid.NewString(),
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   time.Now(),
	}
	if err := a.store.CreateLibrary(c.Request.Context(), lib); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "创建失败（可能重名）: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, lib)
}

func (a *App) deleteLibrary(c *gin.Context) {
	if err := a.importSvc.DeleteLibrary(c.Request.Context(), c.Param("libId")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

type documentSummary struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Format     string `json:"format"`
	ChunkCount int64  `json:"chunk_count"`
	CreatedAt  string `json:"created_at"`
}

func (a *App) listDocuments(c *gin.Context) {
	docs, err := a.store.ListDocuments(c.Request.Context(), c.Param("libId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]documentSummary, 0, len(docs))
	for _, d := range docs {
		chunks, _ := a.store.ListChunksByDoc(c.Request.Context(), d.ID)
		n := int64(0)
		for _, ch := range chunks {
			if ch.Kind == domain.ChunkChild {
				n++
			}
		}
		out = append(out, documentSummary{
			ID: d.ID, Title: d.Title, Format: d.Format,
			ChunkCount: n, CreatedAt: d.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, out)
}

func (a *App) uploadDocument(c *gin.Context) {
	libID := c.Param("libId")
	if _, err := a.store.GetLibrary(c.Request.Context(), libID); err != nil {
		status := http.StatusInternalServerError
		if err == repo.ErrNotFound {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": "知识库不存在"})
		return
	}

	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 file 字段（multipart）"})
		return
	}
	format := ""
	switch {
	case hasSuffixFold(fh.Filename, ".md", ".markdown"):
		format = "md"
	case hasSuffixFold(fh.Filename, ".txt"):
		format = "txt"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 .md / .txt（PDF 与 Anki 在 V1.5）"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取上传文件失败"})
		return
	}
	defer f.Close()

	buf, err := io.ReadAll(io.LimitReader(f, 10<<20+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取内容失败"})
		return
	}
	if len(buf) > 10<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件超过 10MB 上限"})
		return
	}
	title := fh.Filename
	doc, childCount, err := a.importSvc.Import(c.Request.Context(), libID, title, format, string(buf))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id": doc.ID, "title": doc.Title, "format": doc.Format,
		"chunk_count": childCount,
	})
}

func (a *App) getDocument(c *gin.Context) {
	ctx := c.Request.Context()
	doc, err := a.store.GetDocument(ctx, c.Param("docId"))
	if err == repo.ErrNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "文档不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	chunks, err := a.store.ListChunksByDoc(ctx, doc.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"document": doc, "chunks": chunks})
}

func (a *App) deleteDocument(c *gin.Context) {
	if err := a.importSvc.DeleteDocument(c.Request.Context(), c.Param("docId")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func hasSuffixFold(s string, suffixes ...string) bool {
	for _, suf := range suffixes {
		if len(s) >= len(suf) && equalFoldASCII(s[len(s)-len(suf):], suf) {
			return true
		}
	}
	return false
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 32
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
