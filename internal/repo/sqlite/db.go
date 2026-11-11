// Package sqlite 是 repo.Store 的 SQLite 实现（本地版默认存储）。
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/Gulugll/fupan/internal/repo"

	_ "modernc.org/sqlite" // 纯 Go 驱动，免 cgo
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// queryer 抽象 *sql.DB 与 *sql.Tx 的公共能力，实体方法对事务内外透明。
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// store 承载全部实体方法；q 在事务外是 *sql.DB、事务内是 *sql.Tx。
type store struct{ q queryer }

// 编译期断言：*store 实现 repo.Store（硬约束：存储可替换）。
var _ repo.Store = (*store)(nil)

// Open 打开数据库并执行迁移。
// WAL 提升并发读写；foreign_keys 保证溯源外键（cards.source_chunk_id）真正生效。
func Open(dsn string) (*DB, error) {
	d, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	// SQLite 单写者：限制连接数避免 database locked。
	d.SetMaxOpenConns(1)
	if err := migrate(d); err != nil {
		d.Close()
		return nil, err
	}
	return &DB{db: d}, nil
}

// DB 持有连接池；对外暴露非事务 Store 与事务入口。
type DB struct {
	db *sql.DB
}

func (d *DB) Close() error { return d.db.Close() }

// Store 返回非事务的 repo.Store 视图。
func (d *DB) Store() repo.Store { return &store{q: d.db} }

// Tx 在单个事务中执行 fn；fn 收到事务内的 repo.Store 视图。
func (d *DB) Tx(ctx context.Context, fn func(s repo.Store) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(&store{q: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// Tx 对事务内的 store 调用即加入当前事务（不做嵌套事务）。
func (s *store) Tx(ctx context.Context, fn func(inner repo.Store) error) error {
	return fn(s)
}

// migrate 依次执行 migrations/*.sql。靠 schema_migrations 记录已应用文件，
// 因为 ALTER TABLE 等语句不像 CREATE TABLE 有 IF NOT EXISTS，不能每次启动重放。
// 兼容旧库：0001 之前的应用无记录，但其内容全为 IF NOT EXISTS，重放无害。
func migrate(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(name TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("创建 schema_migrations 失败: %w", err)
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("读取迁移目录失败: %w", err)
	}
	for _, e := range entries {
		var name string
		err := d.QueryRow(`SELECT name FROM schema_migrations WHERE name=?`, e.Name()).Scan(&name)
		if err == nil {
			continue // 已应用，跳过
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("查询迁移记录 %s 失败: %w", e.Name(), err)
		}
		sqlBytes, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("读取迁移 %s 失败: %w", e.Name(), err)
		}
		// 0002 发布早期未记录 schema_migrations；已有 category 时只补记录，避免重复 ALTER。
		if e.Name() == "0002_card_category.sql" {
			exists, err := columnExists(d, "cards", "category")
			if err != nil {
				return err
			}
			if exists {
				sqlBytes = nil
			}
		}
		if err := applyMigration(d, e.Name(), sqlBytes); err != nil {
			return err
		}
	}
	return nil
}

func columnExists(d *sql.DB, table, column string) (bool, error) {
	rows, err := d.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func applyMigration(d *sql.DB, name string, sqlBytes []byte) error {
	tx, err := d.Begin()
	if err != nil {
		return fmt.Errorf("开始迁移 %s 事务失败: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()
	if len(sqlBytes) > 0 {
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("执行迁移 %s 失败: %w", name, err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(name) VALUES(?)`, name); err != nil {
		return fmt.Errorf("记录迁移 %s 失败: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交迁移 %s 失败: %w", name, err)
	}
	return nil
}
