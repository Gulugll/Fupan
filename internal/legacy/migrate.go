// Package legacy 提供显式、离线的单用户旧库到 admin 租户迁移。
package legacy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gulugll/fupan/internal/auth"
	"github.com/Gulugll/fupan/internal/config"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
	"github.com/Gulugll/fupan/internal/tenant"
	_ "modernc.org/sqlite"
)

// Result 是 CLI 需要展示的迁移产物与稳定内部用户 ID。
type Result struct {
	Backup string
	Target string
	UserID string
}

var comparedTables = []string{
	"libraries", "documents", "chunks", "cards", "schedules", "review_logs", "card_proposals", "settings",
}

// Migrate 一致性备份旧根库，在临时副本上升级并校验后以不覆盖方式落位。
// 已成功迁移时只返回现有目标，不重新备份或复制。
func Migrate(ctx context.Context, cfg *config.Config) (Result, error) {
	if err := validateConfig(cfg); err != nil {
		return Result{}, err
	}
	sourcePath := cfg.DBPath()
	info, err := os.Stat(sourcePath)
	if err != nil {
		return Result{}, fmt.Errorf("读取旧根库失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Result{}, fmt.Errorf("旧根库不是普通文件: %s", sourcePath)
	}

	control, err := auth.Open(filepath.Join(cfg.DataDir, "control.db"))
	if err != nil {
		return Result{}, err
	}
	defer control.Close()
	user, err := control.UpsertUser(ctx, cfg.OIDC.Issuer, cfg.OIDC.AdminSub, "admin")
	if err != nil {
		return Result{}, fmt.Errorf("映射 admin 用户失败: %w", err)
	}
	targetPath, err := tenant.NewManager(cfg).DBPath(user.ID)
	if err != nil {
		return Result{}, err
	}
	result := Result{Target: targetPath, UserID: user.ID}
	if user.LegacyMigratedAt != nil {
		if info, err := os.Stat(targetPath); err != nil || !info.Mode().IsRegular() {
			return Result{}, fmt.Errorf("已标记迁移但目标库不存在或无效: %s", targetPath)
		}
		return result, nil
	}
	if _, err := os.Lstat(targetPath); err == nil {
		return Result{}, fmt.Errorf("目标库已存在且尚未完成迁移，拒绝覆盖: %s", targetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("检查目标库失败: %w", err)
	}

	backupPath := filepath.Join(cfg.DataDir, fmt.Sprintf(
		"fupan-backup-%s.db", time.Now().UTC().Format("20060102T150405.000000000Z"),
	))
	if err := createBackup(ctx, sourcePath, backupPath); err != nil {
		return Result{}, err
	}
	result.Backup = backupPath
	if err := validateDatabase(ctx, backupPath); err != nil {
		return Result{}, fmt.Errorf("旧库备份校验失败: %w", err)
	}

	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("创建 admin 租户目录失败: %w", err)
	}
	if err := os.Chmod(filepath.Dir(targetDir), 0o700); err != nil {
		return Result{}, fmt.Errorf("保护 users 目录失败: %w", err)
	}
	if err := os.Chmod(targetDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("保护 admin 租户目录失败: %w", err)
	}
	tempFile, err := os.CreateTemp(targetDir, ".fupan-migrate-*.db")
	if err != nil {
		return Result{}, fmt.Errorf("创建迁移临时文件失败: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
		_ = os.Remove(tempPath + "-wal")
		_ = os.Remove(tempPath + "-shm")
	}()
	if err := copyBackup(tempFile, backupPath); err != nil {
		return Result{}, err
	}
	if err := migrateTenant(tempPath); err != nil {
		return Result{}, err
	}
	if err := checkpoint(ctx, tempPath); err != nil {
		return Result{}, err
	}
	if err := validateMigration(ctx, backupPath, tempPath); err != nil {
		return Result{}, fmt.Errorf("迁移结果校验失败: %w", err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return Result{}, fmt.Errorf("保护迁移目标失败: %w", err)
	}
	for _, sidecar := range []string{tempPath + "-wal", tempPath + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("清理迁移 sidecar 失败: %w", err)
		}
	}

	// 同目录硬链接是原子发布且 O_EXCL 等价：目标竞争出现时不会覆盖。
	if err := os.Link(tempPath, targetPath); err != nil {
		return Result{}, fmt.Errorf("原子落位失败（不会覆盖已有目标）: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return Result{}, fmt.Errorf("清理已落位临时文件失败: %w", err)
	}
	if err := control.MarkLegacyMigrated(ctx, user.ID); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("配置不能为空")
	}
	if cfg.NoAuth {
		return fmt.Errorf("NoAuth 模式不得执行旧库迁移")
	}
	for name, value := range map[string]string{
		"issuer": cfg.OIDC.Issuer, "client id": cfg.OIDC.ClientID,
		"client secret": cfg.OIDC.ClientSecret, "redirect URL": cfg.OIDC.RedirectURL,
		"admin sub": cfg.OIDC.AdminSub,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("旧库迁移缺少 OIDC %s", name)
		}
	}
	return nil
}

func openSQLite(path, mode string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := &url.URL{Scheme: "file", Path: absolute}
	query := u.Query()
	query.Set("mode", mode)
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func createBackup(ctx context.Context, sourcePath, backupPath string) (err error) {
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(backupPath)
		}
	}()
	db, err := openSQLite(sourcePath, "ro")
	if err != nil {
		return fmt.Errorf("只读打开旧根库失败: %w", err)
	}
	if _, err = db.ExecContext(ctx, `VACUUM INTO ?`, backupPath); err != nil {
		_ = db.Close()
		return fmt.Errorf("SQLite 一致性备份失败: %w", err)
	}
	if err = db.Close(); err != nil {
		return fmt.Errorf("关闭旧根库备份连接失败: %w", err)
	}
	if err = os.Chmod(backupPath, 0o600); err != nil {
		return fmt.Errorf("保护旧库备份失败: %w", err)
	}
	keep = true
	return nil
}

func copyBackup(destination *os.File, backupPath string) error {
	source, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("打开旧库备份失败: %w", err)
	}
	defer source.Close()
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		return fmt.Errorf("复制旧库备份失败: %w", err)
	}
	if err := destination.Sync(); err != nil {
		_ = destination.Close()
		return fmt.Errorf("同步迁移临时文件失败: %w", err)
	}
	if err := destination.Close(); err != nil {
		return fmt.Errorf("关闭迁移临时文件失败: %w", err)
	}
	return nil
}

func migrateTenant(path string) error {
	db, err := sqlite.Open(path)
	if err != nil {
		return fmt.Errorf("执行租户 migrations 失败: %w", err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("关闭迁移临时库失败: %w", err)
	}
	return nil
}

func checkpoint(ctx context.Context, path string) error {
	db, err := openSQLite(path, "rw")
	if err != nil {
		return fmt.Errorf("打开迁移检查点连接失败: %w", err)
	}
	var busy, logFrames, checkpointed int
	if err := db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		_ = db.Close()
		return fmt.Errorf("迁移临时库 WAL checkpoint 失败: %w", err)
	}
	if busy != 0 {
		_ = db.Close()
		return fmt.Errorf("迁移临时库 WAL checkpoint 被阻塞")
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("关闭迁移检查点连接失败: %w", err)
	}
	return nil
}

func validateDatabase(ctx context.Context, path string) error {
	db, err := openSQLite(path, "ro")
	if err != nil {
		return err
	}
	defer db.Close()
	return checkDatabase(ctx, db)
}

func validateMigration(ctx context.Context, sourcePath, targetPath string) error {
	source, err := openSQLite(sourcePath, "ro")
	if err != nil {
		return fmt.Errorf("打开迁移源快照失败: %w", err)
	}
	defer source.Close()
	if err := checkDatabase(ctx, source); err != nil {
		return fmt.Errorf("迁移源快照: %w", err)
	}
	target, err := openSQLite(targetPath, "ro")
	if err != nil {
		return fmt.Errorf("打开迁移目标失败: %w", err)
	}
	defer target.Close()
	if err := checkDatabase(ctx, target); err != nil {
		return fmt.Errorf("迁移目标: %w", err)
	}
	for _, table := range comparedTables {
		sourceCount, err := tableCount(ctx, source, table)
		if err != nil {
			return err
		}
		targetCount, err := tableCount(ctx, target, table)
		if err != nil {
			return err
		}
		if sourceCount != targetCount {
			return fmt.Errorf("表 %s 行数不一致: 源=%d 目标=%d", table, sourceCount, targetCount)
		}
	}
	return nil
}

func checkDatabase(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("integrity_check 执行失败: %w", err)
	}
	valid := false
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			_ = rows.Close()
			return fmt.Errorf("integrity_check 读取失败: %w", err)
		}
		valid = true
		if result != "ok" {
			_ = rows.Close()
			return fmt.Errorf("integrity_check 失败: %s", result)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("integrity_check 失败: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("integrity_check 关闭失败: %w", err)
	}
	if !valid {
		return fmt.Errorf("integrity_check 未返回结果")
	}

	foreignRows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("foreign_key_check 执行失败: %w", err)
	}
	defer foreignRows.Close()
	if foreignRows.Next() {
		var table, parent string
		var rowID any
		var foreignKeyID int
		if err := foreignRows.Scan(&table, &rowID, &parent, &foreignKeyID); err != nil {
			return fmt.Errorf("foreign_key_check 读取失败: %w", err)
		}
		return fmt.Errorf("foreign_key_check 失败: table=%s row=%v parent=%s", table, rowID, parent)
	}
	if err := foreignRows.Err(); err != nil {
		return fmt.Errorf("foreign_key_check 失败: %w", err)
	}
	return nil
}

func tableCount(ctx context.Context, db *sql.DB, table string) (int, error) {
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
		return 0, fmt.Errorf("检查表 %s 失败: %w", table, err)
	}
	if exists == 0 {
		return 0, nil
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		return 0, fmt.Errorf("统计表 %s 行数失败: %w", table, err)
	}
	return count, nil
}
