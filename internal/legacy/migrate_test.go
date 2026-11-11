package legacy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/auth"
	"github.com/Gulugll/fupan/internal/config"
	"github.com/Gulugll/fupan/internal/domain"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
	"github.com/Gulugll/fupan/internal/service"
	"github.com/google/uuid"
)

var migratedTables = []string{
	"libraries", "documents", "chunks", "cards", "schedules", "review_logs", "card_proposals", "settings",
}

func testConfig(dataDir string) *config.Config {
	return &config.Config{
		DataDir: dataDir,
		OIDC: config.OIDCConfig{
			Issuer: "https://issuer.invalid", ClientID: "fupan", ClientSecret: "secret",
			RedirectURL: "https://fupan.invalid/auth/callback", AdminSub: "admin-sub",
		},
	}
}

func seedAllEntities(t *testing.T, db *sqlite.DB) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store := db.Store()
	if err := store.CreateLibrary(ctx, &domain.Library{ID: "lib-1", Name: "旧库", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateDocument(ctx, &domain.Document{ID: "doc-1", LibraryID: "lib-1", Title: "旧文档", Format: "md", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertChunks(ctx, []domain.Chunk{{
		ID: "chunk-1", DocID: "doc-1", Kind: domain.ChunkChild, Content: "WAL 中最新提交的正文",
		Author: domain.AuthorUser, Version: 1, CreatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateCards(ctx, []domain.Card{{
		ID: "card-1", LibraryID: "lib-1", Front: "问题", Back: "答案", Type: domain.CardQA,
		Category: "legacy", SourceChunkID: "chunk-1", SourceVersion: 1, Status: "active", CreatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSchedule(ctx, &domain.Schedule{CardID: "card-1", State: domain.StateReview, Due: now, Reps: 3}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertReviewLog(ctx, &domain.ReviewLog{ID: "review-1", CardID: "card-1", Rating: 4, ReviewedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProposal(ctx, &domain.CardProposal{
		ID: "proposal-1", LibraryID: "lib-1", CardID: "card-1", Kind: "edit",
		Payload: `{"front":"new"}`, Status: "pending", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSetting(ctx, service.KeyLLMChatModel, "model-from-wal"); err != nil {
		t.Fatal(err)
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func openRaw(t *testing.T, path string, readOnly bool) *sql.DB {
	t.Helper()
	dsn := path
	if readOnly {
		dsn = "file:" + path + "?mode=ro"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func tableCounts(t *testing.T, path string) map[string]int {
	t.Helper()
	db := openRaw(t, path, true)
	counts := make(map[string]int, len(migratedTables))
	for _, table := range migratedTables {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists == 0 {
			counts[table] = 0
			continue
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("读取 %s 行数失败: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s 权限为 %04o，期望 %04o", path, got, want)
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "migrate") || strings.HasSuffix(entry.Name(), "-wal") || strings.HasSuffix(entry.Name(), "-shm") {
			t.Fatalf("迁移后残留临时/WAL 文件: %s", entry.Name())
		}
	}
}

func TestMigrateIncludesCommittedWALAndAllEntities(t *testing.T) {
	cfg := testConfig(t.TempDir())
	source, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	seedAllEntities(t, source)
	if info, err := os.Stat(cfg.DBPath() + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("测试前置条件失败：没有非空 WAL: info=%v err=%v", info, err)
	}
	sourceHash := fileHash(t, cfg.DBPath())
	sourceCounts := tableCounts(t, cfg.DBPath())
	for table, count := range sourceCounts {
		if count != 1 {
			t.Fatalf("测试前置条件失败：%s 应有 1 行，实际 %d", table, count)
		}
	}

	result, err := Migrate(context.Background(), cfg)
	if err != nil {
		t.Fatalf("离线迁移失败: %v", err)
	}
	if _, err := uuid.Parse(result.UserID); err != nil {
		t.Fatalf("Result.UserID 不是内部 UUID: %q", result.UserID)
	}
	wantTarget := filepath.Join(cfg.DataDir, "users", result.UserID, "fupan.db")
	if result.Target != wantTarget || result.Backup == "" || result.Backup == cfg.DBPath() {
		t.Fatalf("Result 路径错误: %+v", result)
	}
	assertMode(t, result.Backup, 0o600)
	assertMode(t, result.Target, 0o600)
	assertMode(t, filepath.Dir(result.Target), 0o700)
	assertMode(t, filepath.Dir(filepath.Dir(result.Target)), 0o700)
	assertNoTempFiles(t, filepath.Dir(result.Target))
	for table, want := range sourceCounts {
		if got := tableCounts(t, result.Target)[table]; got != want {
			t.Errorf("目标 %s 行数=%d，源=%d", table, got, want)
		}
	}
	target := openRaw(t, result.Target, true)
	var model, content string
	if err := target.QueryRow(`SELECT value FROM settings WHERE key=?`, service.KeyLLMChatModel).Scan(&model); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(`SELECT content FROM chunks WHERE id='chunk-1'`).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if model != "model-from-wal" || content != "WAL 中最新提交的正文" {
		t.Fatalf("WAL 最新提交未迁移: model=%q content=%q", model, content)
	}
	if got := fileHash(t, cfg.DBPath()); got != sourceHash {
		t.Fatal("迁移修改了源主库")
	}
	for table, want := range sourceCounts {
		if got := tableCounts(t, cfg.DBPath())[table]; got != want {
			t.Errorf("迁移后源 %s 行数=%d，原值=%d", table, got, want)
		}
	}

	control, err := auth.Open(filepath.Join(cfg.DataDir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := control.UserByID(context.Background(), result.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if user.LegacyMigratedAt == nil || user.Issuer != cfg.OIDC.Issuer || user.Sub != cfg.OIDC.AdminSub {
		t.Fatalf("迁移标记或 admin 映射错误: %+v", user)
	}
	markedAt := *user.LegacyMigratedAt
	_ = control.Close()

	// 迁移后的新数据必须保留，成功重跑只返回现状，不再备份或覆盖。
	targetDB, err := sqlite.Open(result.Target)
	if err != nil {
		t.Fatal(err)
	}
	if err := targetDB.Store().PutSetting(context.Background(), "after.migration", "keep-me"); err != nil {
		t.Fatal(err)
	}
	if err := targetDB.Close(); err != nil {
		t.Fatal(err)
	}
	targetHash := fileHash(t, result.Target)
	backupsBefore, err := filepath.Glob(filepath.Join(cfg.DataDir, "fupan-backup-*.db"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Migrate(context.Background(), cfg)
	if err != nil {
		t.Fatalf("成功后的重跑应幂等: %v", err)
	}
	if second.UserID != result.UserID || second.Target != result.Target {
		t.Fatalf("重跑改变了用户或目标: first=%+v second=%+v", result, second)
	}
	if got := fileHash(t, result.Target); got != targetHash {
		t.Fatal("成功重跑覆盖了目标新数据")
	}
	var kept string
	if err := target.QueryRow(`SELECT value FROM settings WHERE key='after.migration'`).Scan(&kept); err != nil || kept != "keep-me" {
		t.Fatalf("重跑后目标新数据丢失: value=%q err=%v", kept, err)
	}
	backupsAfter, err := filepath.Glob(filepath.Join(cfg.DataDir, "fupan-backup-*.db"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backupsAfter) != len(backupsBefore) {
		t.Fatalf("幂等重跑不应新增备份: before=%v after=%v", backupsBefore, backupsAfter)
	}
	control, err = auth.Open(filepath.Join(cfg.DataDir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	user, err = control.UserByID(context.Background(), result.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if user.LegacyMigratedAt == nil || !user.LegacyMigratedAt.Equal(markedAt) {
		t.Fatalf("幂等重跑改写了首次迁移时间: before=%v after=%v", markedAt, user.LegacyMigratedAt)
	}
}

func TestMigrateUpgradesOldSchemaWithoutMigrationRecords(t *testing.T) {
	cfg := testConfig(t.TempDir())
	source, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Store().CreateLibrary(context.Background(), &domain.Library{ID: "old-lib", Name: "旧版本", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	raw := openRaw(t, cfg.DBPath(), false)
	if _, err := raw.Exec(`DROP TABLE card_proposals; DELETE FROM schema_migrations WHERE name IN ('0002_card_category.sql','0003_card_proposals.sql')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	sourceHash := fileHash(t, cfg.DBPath())

	result, err := Migrate(context.Background(), cfg)
	if err != nil {
		t.Fatalf("旧版本库升级失败: %v", err)
	}
	target := openRaw(t, result.Target, true)
	var categoryExists, proposalExists, migrationCount int
	if err := target.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cards') WHERE name='category'`).Scan(&categoryExists); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='card_proposals'`).Scan(&proposalExists); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if categoryExists != 1 || proposalExists != 1 || migrationCount != 3 {
		t.Fatalf("目标未完整升级: category=%d proposals=%d migrations=%d", categoryExists, proposalExists, migrationCount)
	}
	if got := fileHash(t, cfg.DBPath()); got != sourceHash {
		t.Fatal("升级过程修改了旧源库")
	}
	raw = openRaw(t, cfg.DBPath(), true)
	var sourceProposals int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='card_proposals'`).Scan(&sourceProposals); err != nil {
		t.Fatal(err)
	}
	if sourceProposals != 0 {
		t.Fatal("不得在源库补建新版本表")
	}
}

func TestMigrateRefusesExistingTargetWithoutOverwrite(t *testing.T) {
	cfg := testConfig(t.TempDir())
	source, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	control, err := auth.Open(filepath.Join(cfg.DataDir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := control.UpsertUser(context.Background(), cfg.OIDC.Issuer, cfg.OIDC.AdminSub, "admin")
	if err != nil {
		t.Fatal(err)
	}
	_ = control.Close()
	target := filepath.Join(cfg.DataDir, "users", user.ID, "fupan.db")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("existing tenant data")
	if err := os.WriteFile(target, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	sourceHash := fileHash(t, cfg.DBPath())

	if _, err := Migrate(context.Background(), cfg); err == nil {
		t.Fatal("目标存在且未完成迁移时必须拒绝")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(sentinel) {
		t.Fatalf("已有目标被覆盖: %q err=%v", got, err)
	}
	if got := fileHash(t, cfg.DBPath()); got != sourceHash {
		t.Fatal("拒绝目标时修改了源库")
	}
	control, err = auth.Open(filepath.Join(cfg.DataDir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	user, err = control.UserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if user.LegacyMigratedAt != nil {
		t.Fatal("拒绝覆盖不得写入迁移完成标记")
	}
}

func TestMigrateFailureLeavesNoTargetAndCanRetry(t *testing.T) {
	cfg := testConfig(t.TempDir())
	source, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	raw := openRaw(t, cfg.DBPath(), false)
	if _, err := raw.Exec(`INSERT INTO documents(id,library_id,title,format,created_at) VALUES('orphan','missing','bad','md','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	sourceHash := fileHash(t, cfg.DBPath())

	if _, err := Migrate(context.Background(), cfg); err == nil {
		t.Fatal("foreign_key_check 失败时迁移必须失败")
	}
	control, err := auth.Open(filepath.Join(cfg.DataDir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := control.UpsertUser(context.Background(), cfg.OIDC.Issuer, cfg.OIDC.AdminSub, "admin")
	if err != nil {
		t.Fatal(err)
	}
	_ = control.Close()
	targetDir := filepath.Join(cfg.DataDir, "users", user.ID)
	if _, err := os.Stat(filepath.Join(targetDir, "fupan.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("失败后不得留下目标库: %v", err)
	}
	assertNoTempFiles(t, targetDir)
	if got := fileHash(t, cfg.DBPath()); got != sourceHash {
		t.Fatal("失败迁移修改了源库")
	}

	// 两次尝试之间修复测试源；单次迁移本身始终只读。
	raw = openRaw(t, cfg.DBPath(), false)
	if _, err := raw.Exec(`DELETE FROM documents WHERE id='orphan'; INSERT INTO libraries(id,name,created_at) VALUES('fixed','fixed','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(context.Background(), cfg)
	if err != nil {
		t.Fatalf("清理失败原因后应可重试: %v", err)
	}
	assertNoTempFiles(t, filepath.Dir(result.Target))
	if got := tableCounts(t, result.Target)["libraries"]; got != 1 {
		t.Fatalf("重试目标数据错误: libraries=%d", got)
	}
}

func TestValidateMigrationRejectsIntegrityForeignKeyAndCountFailures(t *testing.T) {
	t.Run("行数不一致", func(t *testing.T) {
		dir := t.TempDir()
		sourcePath := filepath.Join(dir, "source.db")
		targetPath := filepath.Join(dir, "target.db")
		source, err := sqlite.Open(sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := source.Store().CreateLibrary(context.Background(), &domain.Library{ID: "lib", Name: "source", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		_ = source.Close()
		target, err := sqlite.Open(targetPath)
		if err != nil {
			t.Fatal(err)
		}
		_ = target.Close()
		if err := validateMigration(context.Background(), sourcePath, targetPath); err == nil || !strings.Contains(err.Error(), "行数") {
			t.Fatalf("行数不一致必须失败: %v", err)
		}
	})

	t.Run("外键不一致", func(t *testing.T) {
		dir := t.TempDir()
		sourcePath := filepath.Join(dir, "source.db")
		targetPath := filepath.Join(dir, "target.db")
		for _, path := range []string{sourcePath, targetPath} {
			db, err := sqlite.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			_ = db.Close()
		}
		raw := openRaw(t, targetPath, false)
		if _, err := raw.Exec(`INSERT INTO documents(id,library_id,title,format,created_at) VALUES('orphan','missing','bad','md','2026-01-01')`); err != nil {
			t.Fatal(err)
		}
		_ = raw.Close()
		if err := validateMigration(context.Background(), sourcePath, targetPath); err == nil || !strings.Contains(err.Error(), "foreign") {
			t.Fatalf("foreign_key_check 必须失败: %v", err)
		}
	})

	t.Run("完整性损坏", func(t *testing.T) {
		dir := t.TempDir()
		sourcePath := filepath.Join(dir, "source.db")
		source, err := sqlite.Open(sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		_ = source.Close()
		targetPath := filepath.Join(dir, "target.db")
		if err := os.WriteFile(targetPath, []byte("not a sqlite database"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := validateMigration(context.Background(), sourcePath, targetPath); err == nil {
			t.Fatal("integrity_check 或打开损坏库必须失败")
		}
	})
}

func TestMigrateRejectsInvalidCoreConfiguration(t *testing.T) {
	for _, mutate := range []func(*config.Config){
		func(cfg *config.Config) { cfg.NoAuth = true },
		func(cfg *config.Config) { cfg.OIDC.Issuer = "" },
		func(cfg *config.Config) { cfg.OIDC.AdminSub = "" },
	} {
		cfg := testConfig(t.TempDir())
		mutate(cfg)
		if _, err := Migrate(context.Background(), cfg); err == nil {
			t.Fatalf("核心配置无效时必须拒绝: %+v", cfg)
		}
		if _, err := os.Stat(filepath.Join(cfg.DataDir, "control.db")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("配置错误不得创建控制库: %v", err)
		}
	}
}
