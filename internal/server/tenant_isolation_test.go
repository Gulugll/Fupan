package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/auth"
	"github.com/Gulugll/fupan/internal/domain"
	"github.com/Gulugll/fupan/internal/service"
	"github.com/Gulugll/fupan/internal/tenant"
)

type seededTenant struct {
	user       auth.User
	token      string
	services   *tenant.Services
	libraryID  string
	documentID string
	chunkID    string
	cardID     string
	proposalID string
	name       string
}

func seedTenant(t *testing.T, env *oidcTestEnv, sub, name string) seededTenant {
	t.Helper()
	user, token := env.createSession(t, sub, name)
	services, release, err := env.app.manager.Acquire(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	// newOIDCTestApp 已先注册 App.Close，release 会先于它执行。
	t.Cleanup(release)
	seed := seededTenant{
		user: user, token: token, services: services, name: name,
		libraryID: sub + "-library", documentID: sub + "-document", chunkID: sub + "-chunk",
		cardID: sub + "-card", proposalID: sub + "-proposal",
	}
	ctx := t.Context()
	if err := services.Store.CreateLibrary(ctx, &domain.Library{ID: seed.libraryID, Name: name + " Library", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := services.Store.CreateDocument(ctx, &domain.Document{ID: seed.documentID, LibraryID: seed.libraryID, Title: name + " Document", Format: "md", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := services.Store.InsertChunks(ctx, []domain.Chunk{{
		ID: seed.chunkID, DocID: seed.documentID, Kind: domain.ChunkChild, Content: name + " private source",
		Author: domain.AuthorUser, Version: 1, CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := services.Store.CreateCards(ctx, []domain.Card{{
		ID: seed.cardID, LibraryID: seed.libraryID, Front: name + " private card", Back: name + " answer",
		Type: domain.CardQA, SourceChunkID: seed.chunkID, SourceVersion: 1, Status: "active", CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{"front": name + " proposal", "back": "back", "category": "private"})
	if err != nil {
		t.Fatal(err)
	}
	if err := services.Store.CreateProposal(ctx, &domain.CardProposal{
		ID: seed.proposalID, LibraryID: seed.libraryID, CardID: seed.cardID, Kind: "edit",
		Payload: string(payload), Status: "pending", Rationale: name + " only", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := services.Settings.Set(ctx, service.KeyLLMChatModel, name+"-model"); err != nil {
		t.Fatal(err)
	}
	return seed
}

func TestTwoUsersSettingsLibraryDocumentCardAndProposalIsolation(t *testing.T) {
	env := newOIDCTestApp(t)
	alice := seedTenant(t, env, "alice-sub", "Alice")
	bob := seedTenant(t, env, "bob-sub", "Bob")

	for _, current := range []seededTenant{alice, bob} {
		w := serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/settings", current.token, "", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), current.name+"-model") {
			t.Fatalf("%s 设置被串读: %d %s", current.name, w.Code, w.Body.String())
		}
		other := alice
		if current.user.ID == alice.user.ID {
			other = bob
		}
		if strings.Contains(w.Body.String(), other.name+"-model") {
			t.Fatalf("%s 设置包含其他用户数据: %s", current.name, w.Body.String())
		}

		w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/libraries", current.token, "", nil))
		var libraries []domain.Library
		if err := json.Unmarshal(w.Body.Bytes(), &libraries); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || len(libraries) != 1 || libraries[0].ID != current.libraryID {
			t.Fatalf("%s 知识库列表未隔离: %d %+v", current.name, w.Code, libraries)
		}

		w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/documents/"+current.documentID, current.token, "", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), current.name+" Document") {
			t.Fatalf("%s 自有文档不可读: %d %s", current.name, w.Code, w.Body.String())
		}
		w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/documents/"+other.documentID, current.token, "", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s 可通过 direct-ID 读取其他用户文档: %d %s", current.name, w.Code, w.Body.String())
		}
		w = serveRequest(env.router, authenticatedRequest(http.MethodPost, "/api/libraries/"+other.libraryID+"/documents", current.token, "https://fupan.example.com", bytes.NewBufferString("not multipart")))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s 可向其他用户知识库上传: %d %s", current.name, w.Code, w.Body.String())
		}

		w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/libraries/"+current.libraryID+"/cards", current.token, "", nil))
		var cards []domain.Card
		if err := json.Unmarshal(w.Body.Bytes(), &cards); err != nil {
			t.Fatal(err)
		}
		if len(cards) != 1 || cards[0].ID != current.cardID {
			t.Fatalf("%s 卡片列表未隔离: %+v", current.name, cards)
		}
		w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/cards/"+other.cardID+"/source", current.token, "", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s 可通过 direct-ID 读取其他用户卡片: %d", current.name, w.Code)
		}
		w = serveRequest(env.router, authenticatedRequest(http.MethodPut, "/api/cards/"+other.cardID, current.token, "https://fupan.example.com", bytes.NewBufferString(`{"front":"cross-user overwrite"}`)))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s 可通过 direct-ID 修改其他用户卡片: %d %s", current.name, w.Code, w.Body.String())
		}

		w = serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/libraries/"+current.libraryID+"/proposals", current.token, "", nil))
		var proposals []domain.CardProposal
		if err := json.Unmarshal(w.Body.Bytes(), &proposals); err != nil {
			t.Fatal(err)
		}
		if len(proposals) != 1 || proposals[0].ID != current.proposalID {
			t.Fatalf("%s 提案列表未隔离: %+v", current.name, proposals)
		}
		w = serveRequest(env.router, authenticatedRequest(http.MethodPost, "/api/proposals/"+other.proposalID+"/reject", current.token, "https://fupan.example.com", nil))
		if w.Code == http.StatusOK {
			t.Fatalf("%s 可处理其他用户提案: %d %s", current.name, w.Code, w.Body.String())
		}
		otherProposal, err := other.services.Store.GetProposal(t.Context(), other.proposalID)
		if err != nil || otherProposal.Status != "pending" {
			t.Fatalf("跨用户操作修改了提案: %+v err=%v", otherProposal, err)
		}
	}

	w := serveRequest(env.router, authenticatedRequest(http.MethodPost, "/api/proposals/"+alice.proposalID+"/reject", alice.token, "https://fupan.example.com", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("本人提案应可处理: %d %s", w.Code, w.Body.String())
	}
	bobCard, err := bob.services.Store.GetCard(t.Context(), bob.cardID)
	if err != nil || bobCard.Front != "Bob private card" {
		t.Fatalf("其他用户卡片被修改: %+v err=%v", bobCard, err)
	}
}

func TestTenantInitializationFailureReturns500WithoutLegacyFallback(t *testing.T) {
	env := newOIDCTestApp(t)
	_, rawToken := env.createSession(t, "user-sub", "Failure User")
	if err := env.app.manager.Close(); err != nil {
		t.Fatal(err)
	}
	w := serveRequest(env.router, authenticatedRequest(http.MethodGet, "/api/settings", rawToken, "", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("租户初始化故障必须 500: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(env.cfg.DBPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("租户故障不得回退根旧库: %v", err)
	}
}
