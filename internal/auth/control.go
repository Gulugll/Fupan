// Package auth 提供独立控制库、OIDC 登录和随机会话。
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const SessionTTL = 7 * 24 * time.Hour

var (
	ErrInvalidSession = errors.New("无效或过期会话")
	ErrInvalidState   = errors.New("无效或过期 OIDC state")
	ErrNotAllowed     = errors.New("用户不在白名单")
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Control struct {
	db *sql.DB
}

type User struct {
	ID               string
	Issuer           string
	Sub              string
	DisplayName      string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	LegacyMigratedAt *time.Time
}

type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Open 打开独立控制库并执行认证专用迁移。
func Open(path string) (*Control, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("打开控制库失败: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Control{db: db}, nil
}

func (c *Control) Close() error {
	return c.db.Close()
}

// UpsertUser 按 (issuer,sub) 更新资料，同时保持内部 UUID 稳定。
func (c *Control) UpsertUser(ctx context.Context, issuer, sub, displayName string) (User, error) {
	if issuer == "" || sub == "" {
		return User{}, fmt.Errorf("issuer 和 sub 不能为空")
	}
	now := time.Now().UTC()
	row := c.db.QueryRowContext(ctx, `
		INSERT INTO users(id, issuer, sub, display_name, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(issuer, sub) DO UPDATE SET
			display_name = excluded.display_name,
			updated_at = excluded.updated_at
		RETURNING id, issuer, sub, display_name, created_at, updated_at, legacy_migrated_at`,
		uuid.NewString(), issuer, sub, displayName, now, now)
	return scanUser(row)
}

// UserByID 按稳定内部 UUID 查询用户及旧库迁移状态。
func (c *Control) UserByID(ctx context.Context, userID string) (User, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT id, issuer, sub, display_name, created_at, updated_at, legacy_migrated_at
		FROM users WHERE id = ?`, userID)
	user, err := scanUser(row)
	if err != nil {
		return User{}, fmt.Errorf("查询用户失败: %w", err)
	}
	return user, nil
}

// MarkLegacyMigrated 记录首次成功迁移时间；重复调用不改变原时间。
func (c *Control) MarkLegacyMigrated(ctx context.Context, userID string) error {
	result, err := c.db.ExecContext(ctx, `
		UPDATE users SET legacy_migrated_at = COALESCE(legacy_migrated_at, ?) WHERE id = ?`,
		time.Now().UTC(), userID)
	if err != nil {
		return fmt.Errorf("标记旧库迁移失败: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("确认旧库迁移标记失败: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("标记旧库迁移失败: 用户不存在")
	}
	return nil
}

// CreateSession 返回数据库记录和只返回给调用者一次的原始 256-bit token。
func (c *Control) CreateSession(ctx context.Context, userID string) (Session, string, error) {
	rawToken, err := randomToken()
	if err != nil {
		return Session{}, "", err
	}
	now := time.Now().UTC()
	if _, err := c.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now); err != nil {
		return Session{}, "", fmt.Errorf("清理过期会话失败: %w", err)
	}
	session := Session{
		ID: uuid.NewString(), UserID: userID,
		CreatedAt: now, ExpiresAt: now.Add(SessionTTL),
	}
	if _, err := c.db.ExecContext(ctx, `
		INSERT INTO sessions(id, token_hash, user_id, expires_at, created_at)
		VALUES(?, ?, ?, ?, ?)`, session.ID, hashValue(rawToken), userID, session.ExpiresAt, now); err != nil {
		return Session{}, "", fmt.Errorf("创建会话失败: %w", err)
	}
	return session, rawToken, nil
}

func (c *Control) ResolveSession(ctx context.Context, rawToken string) (User, error) {
	row := c.db.QueryRowContext(ctx, `
		SELECT u.id, u.issuer, u.sub, u.display_name, u.created_at, u.updated_at, u.legacy_migrated_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, hashValue(rawToken), time.Now().UTC())
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrInvalidSession
	}
	if err != nil {
		return User{}, fmt.Errorf("解析会话失败: %w", err)
	}
	return user, nil
}

func (c *Control) DeleteSession(ctx context.Context, rawToken string) error {
	if _, err := c.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashValue(rawToken)); err != nil {
		return fmt.Errorf("删除会话失败: %w", err)
	}
	return nil
}

func scanUser(row *sql.Row) (User, error) {
	var user User
	var legacyMigratedAt sql.NullTime
	if err := row.Scan(&user.ID, &user.Issuer, &user.Sub, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt, &legacyMigratedAt); err != nil {
		return User{}, err
	}
	if legacyMigratedAt.Valid {
		value := legacyMigratedAt.Time
		user.LegacyMigratedAt = &value
	}
	return user, nil
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("生成随机值失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashValue(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(name TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("创建控制库迁移表失败: %w", err)
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("读取控制库迁移目录失败: %w", err)
	}
	for _, entry := range entries {
		var name string
		err := db.QueryRow(`SELECT name FROM schema_migrations WHERE name = ?`, entry.Name()).Scan(&name)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("查询控制库迁移 %s 失败: %w", entry.Name(), err)
		}
		contents, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("读取控制库迁移 %s 失败: %w", entry.Name(), err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("开始控制库迁移 %s 失败: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(string(contents)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行控制库迁移 %s 失败: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(name) VALUES(?)`, entry.Name()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("记录控制库迁移 %s 失败: %w", entry.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交控制库迁移 %s 失败: %w", entry.Name(), err)
		}
	}
	return nil
}
