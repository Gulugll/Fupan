// Package tenant 按内部用户 UUID 懒加载并隔离每用户服务。
package tenant

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Gulugll/fupan/internal/config"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
	"github.com/Gulugll/fupan/internal/vector"
	"github.com/google/uuid"
)

const (
	janitorInterval = 5 * time.Minute
	tenantIdleTTL   = 30 * time.Minute
)

type entry struct {
	services *Services
	lastUsed time.Time
	refs     int
}

// Manager 缓存少量用户的 Services；全局锁保证同 UID 只初始化一次。
type Manager struct {
	cfg     *config.Config
	mu      sync.Mutex
	entries map[string]*entry
	open    func(string) (*sqlite.DB, error)
	closed  bool

	activeRefs int
	refsCond   *sync.Cond
	closeOnce  sync.Once
	closeErr   error

	stopJanitor chan struct{}
	janitorDone chan struct{}
}

func NewManager(cfg *config.Config) *Manager {
	m := &Manager{
		cfg:         cfg,
		entries:     make(map[string]*entry),
		open:        sqlite.Open,
		stopJanitor: make(chan struct{}),
		janitorDone: make(chan struct{}),
	}
	m.refsCond = sync.NewCond(&m.mu)
	go m.runJanitor()
	return m
}

// DBPath 返回规范内部 UUID 对应的私有数据库路径，不创建任何目录。
func (m *Manager) DBPath(userID string) (string, error) {
	parsed, err := uuid.Parse(userID)
	if err != nil || parsed.String() != userID {
		return "", fmt.Errorf("无效的内部用户 UUID")
	}
	return filepath.Join(m.cfg.DataDir, "users", userID, "fupan.db"), nil
}

// Acquire 懒加载并引用一个租户；调用方必须在用完后执行返回的幂等 release。
func (m *Manager) Acquire(userID string) (*Services, func(), error) {
	path, err := m.DBPath(userID)
	if err != nil {
		return nil, nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, fmt.Errorf("租户管理器已关闭")
	}

	cached := m.entries[userID]
	if cached == nil {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, nil, fmt.Errorf("创建租户目录失败: %w", err)
		}
		if err := os.Chmod(filepath.Dir(dir), 0o700); err != nil {
			return nil, nil, fmt.Errorf("保护租户根目录失败: %w", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, nil, fmt.Errorf("保护租户目录失败: %w", err)
		}
		db, err := m.open(path)
		if err != nil {
			if db != nil {
				_ = db.Close()
			}
			return nil, nil, fmt.Errorf("初始化租户数据库失败: %w", err)
		}
		cached = &entry{services: NewServices(db, m.cfg, vector.NoopStore{})}
		m.entries[userID] = cached
	}

	cached.refs++
	cached.lastUsed = time.Now()
	m.activeRefs++
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			m.mu.Lock()
			cached.refs--
			cached.lastUsed = time.Now()
			m.activeRefs--
			if m.activeRefs == 0 {
				m.refsCond.Broadcast()
			}
			m.mu.Unlock()
		})
	}
	return cached.services, release, nil
}

func (m *Manager) runJanitor() {
	ticker := time.NewTicker(janitorInterval)
	defer ticker.Stop()
	defer close(m.janitorDone)
	for {
		select {
		case now := <-ticker.C:
			m.sweepIdle(now)
		case <-m.stopJanitor:
			return
		}
	}
}

// sweepIdle 关闭空闲租户连接并保留磁盘数据库；部分关闭失败也移除缓存，避免复用坏连接。
func (m *Manager) sweepIdle(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	for userID, cached := range m.entries {
		if cached.refs != 0 || now.Sub(cached.lastUsed) < tenantIdleTTL {
			continue
		}
		if err := cached.services.Close(); err != nil {
			log.Printf("关闭空闲租户 %s 失败，移除缓存以便下次重开: %v", userID, err)
		}
		delete(m.entries, userID)
	}
}

// Close 停止新引用和 janitor，等待活动引用释放后关闭全部缓存资源。
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		close(m.stopJanitor)
		m.mu.Unlock()

		// janitor 的 sweep 也需要 m.mu，等待退出时不能持锁。
		<-m.janitorDone

		m.mu.Lock()
		for m.activeRefs > 0 {
			m.refsCond.Wait()
		}
		entries := make([]*entry, 0, len(m.entries))
		for _, cached := range m.entries {
			entries = append(entries, cached)
		}
		m.mu.Unlock()

		var errs []error
		for _, cached := range entries {
			if err := cached.services.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		m.mu.Lock()
		clear(m.entries)
		m.mu.Unlock()
		m.closeErr = errors.Join(errs...)
	})
	return m.closeErr
}
