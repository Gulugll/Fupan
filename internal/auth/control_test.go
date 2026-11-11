package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestControl(t *testing.T) *Control {
	t.Helper()
	control, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatalf("打开控制库失败: %v", err)
	}
	t.Cleanup(func() { _ = control.Close() })
	return control
}

func TestUpsertUserKeepsInternalIDStable(t *testing.T) {
	control := newTestControl(t)
	ctx := context.Background()
	first, err := control.UpsertUser(ctx, "https://issuer.example.com", "sub-1", "Alice")
	if err != nil {
		t.Fatalf("首次创建用户失败: %v", err)
	}
	if _, err := uuid.Parse(first.ID); err != nil {
		t.Fatalf("内部用户 ID 不是 UUID: %q", first.ID)
	}
	second, err := control.UpsertUser(ctx, first.Issuer, first.Sub, "Alice Updated")
	if err != nil {
		t.Fatalf("更新用户失败: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("upsert 改变了内部 ID: %s -> %s", first.ID, second.ID)
	}
	if second.DisplayName != "Alice Updated" {
		t.Fatalf("显示名未更新: %q", second.DisplayName)
	}
	other, err := control.UpsertUser(ctx, first.Issuer, "sub-2", "Bob")
	if err != nil {
		t.Fatalf("创建第二个用户失败: %v", err)
	}
	if other.ID == first.ID {
		t.Fatal("不同 sub 不得共用内部 ID")
	}
	var count int
	if err := control.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("(issuer,sub) 唯一性失效，用户数为 %d", count)
	}
}

func TestSessionRandomHashedAndLifecycle(t *testing.T) {
	control := newTestControl(t)
	ctx := context.Background()
	user, err := control.UpsertUser(ctx, "https://issuer.example.com", "sub-1", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	first, rawFirst, err := control.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	_, rawSecond, err := control.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatalf("创建第二个会话失败: %v", err)
	}
	if rawFirst == rawSecond {
		t.Fatal("随机会话 token 重复")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(rawFirst)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("会话 token 不是 256-bit 随机值: len=%d err=%v", len(decoded), err)
	}
	if lifetime := time.Until(first.ExpiresAt); lifetime < 7*24*time.Hour-time.Minute || lifetime > 7*24*time.Hour+time.Minute {
		t.Fatalf("会话有效期不是 7 天: %v", lifetime)
	}
	var storedHash string
	if err := control.db.QueryRow(`SELECT token_hash FROM sessions WHERE id = ?`, first.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawFirst)))
	if storedHash != wantHash || storedHash == rawFirst {
		t.Fatalf("数据库未只存 token 哈希: stored=%q want=%q", storedHash, wantHash)
	}
	resolved, err := control.ResolveSession(ctx, rawFirst)
	if err != nil || resolved.ID != user.ID {
		t.Fatalf("解析会话失败: user=%+v err=%v", resolved, err)
	}

	tampered := "A" + rawFirst[1:]
	if tampered == rawFirst {
		tampered = "B" + rawFirst[1:]
	}
	if _, err := control.ResolveSession(ctx, tampered); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("篡改 token 应无效: %v", err)
	}
	if _, err := control.db.Exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Hour).UTC(), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := control.ResolveSession(ctx, rawFirst); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("过期 token 应无效: %v", err)
	}
	if err := control.DeleteSession(ctx, rawSecond); err != nil {
		t.Fatalf("删除会话失败: %v", err)
	}
	if _, err := control.ResolveSession(ctx, rawSecond); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("删除后 token 应无效: %v", err)
	}
}

func TestMarkLegacyMigratedIsQueryableAndIdempotent(t *testing.T) {
	control := newTestControl(t)
	ctx := context.Background()
	user, err := control.UpsertUser(ctx, "https://issuer.example.com", "admin-sub", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := control.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LegacyMigratedAt != nil {
		t.Fatal("新用户不应有旧库迁移标记")
	}
	if err := control.MarkLegacyMigrated(ctx, user.ID); err != nil {
		t.Fatalf("写入迁移标记失败: %v", err)
	}
	got, err = control.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LegacyMigratedAt == nil {
		t.Fatal("迁移标记不可查询")
	}
	first := *got.LegacyMigratedAt
	if err := control.MarkLegacyMigrated(ctx, user.ID); err != nil {
		t.Fatalf("重复标记应幂等: %v", err)
	}
	got, err = control.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LegacyMigratedAt == nil || !got.LegacyMigratedAt.Equal(first) {
		t.Fatalf("重复标记改变了首次时间: first=%v got=%v", first, got.LegacyMigratedAt)
	}
	if _, err := control.UserByID(ctx, uuid.NewString()); err == nil {
		t.Fatal("查询不存在用户必须报错")
	}
	if err := control.MarkLegacyMigrated(ctx, uuid.NewString()); err == nil {
		t.Fatal("标记不存在用户必须报错")
	}
}

func TestControlOperationsFailAfterClose(t *testing.T) {
	control := newTestControl(t)
	ctx := context.Background()
	user, err := control.UpsertUser(ctx, "https://issuer.example.com", "sub-1", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	_, rawToken, err := control.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := control.UpsertUser(ctx, user.Issuer, user.Sub, user.DisplayName); err == nil {
		t.Fatal("关闭后 upsert 必须报错")
	}
	if _, _, err := control.CreateSession(ctx, user.ID); err == nil {
		t.Fatal("关闭后创建会话必须报错")
	}
	if _, err := control.ResolveSession(ctx, rawToken); err == nil || errors.Is(err, ErrInvalidSession) {
		t.Fatalf("关闭后解析必须返回存储故障而非匿名降级: %v", err)
	}
	if err := control.DeleteSession(ctx, rawToken); err == nil {
		t.Fatal("关闭后删除会话必须报错")
	}
}
