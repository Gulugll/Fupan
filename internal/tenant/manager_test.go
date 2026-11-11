package tenant

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gulugll/fupan/internal/config"
	"github.com/Gulugll/fupan/internal/domain"
	sqlite "github.com/Gulugll/fupan/internal/repo/sqlite"
	"github.com/Gulugll/fupan/internal/service"
	"github.com/Gulugll/fupan/internal/vector"
	"github.com/google/uuid"
)

func testConfig(dataDir string) *config.Config {
	return &config.Config{DataDir: dataDir, NoAuth: false, Retention: 0.9, DailyNewLimit: 20, EmbedDim: 2}
}

func acquireForTest(t *testing.T, manager *Manager, userID string) *Services {
	t.Helper()
	services, release, err := manager.Acquire(userID)
	if err != nil {
		t.Fatalf("Acquire(%q) 失败: %v", userID, err)
	}
	if release == nil {
		t.Fatal("Acquire 成功时必须返回 release")
	}
	// Cleanup 后进先出：调用方应先注册 Manager.Close，再调用本 helper。
	t.Cleanup(release)
	return services
}

func TestManagerAcceptsOnlyCanonicalUUIDAndUsesTenantPath(t *testing.T) {
	dataDir := t.TempDir()
	manager := NewManager(testConfig(dataDir))
	t.Cleanup(func() { _ = manager.Close() })
	uid := uuid.NewString()

	path, err := manager.DBPath(uid)
	if err != nil {
		t.Fatalf("规范 UUID 应被接受: %v", err)
	}
	want := filepath.Join(dataDir, "users", uid, "fupan.db")
	if path != want {
		t.Fatalf("租户数据库路径 = %q, 期望 %q", path, want)
	}
	for _, invalid := range []string{"", "../" + uid, uid + "/../other", "not-a-uuid", uuid.New().String()[:32], "urn:uuid:" + uid, upperASCII(uid)} {
		if services, release, err := manager.Acquire(invalid); err == nil {
			if release != nil {
				release()
			}
			t.Errorf("非规范用户 ID %q 必须被拒绝: services=%v", invalid, services)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "users")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("无效用户 ID %q 不得创建 users 目录: %v", invalid, err)
		}
	}
}

func TestManagerConcurrentInitializationOpensAndMigratesOnce(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	var opens atomic.Int32
	manager.open = func(path string) (*sqlite.DB, error) {
		opens.Add(1)
		time.Sleep(10 * time.Millisecond)
		return sqlite.Open(path)
	}
	uid := uuid.NewString()
	const goroutines = 100
	results := make(chan *Services, goroutines)
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			services, release, err := manager.Acquire(uid)
			if release != nil {
				release()
			}
			if err == nil && release == nil {
				err = errors.New("Acquire 成功时 release 为 nil")
			}
			results <- services
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发初始化失败: %v", err)
		}
	}
	var first *Services
	for services := range results {
		if first == nil {
			first = services
		}
		if services != first {
			t.Fatal("同一 UID 必须复用同一份 Services")
		}
	}
	if opens.Load() != 1 {
		t.Fatalf("同一 UID 数据库打开/迁移次数 = %d, 期望 1", opens.Load())
	}
	if first == nil || first.DB == nil || first.Store == nil || first.Settings == nil || first.Import == nil || first.Card == nil || first.Review == nil || first.Agent == nil || first.Proposal == nil || first.Vectors == nil {
		t.Fatalf("Services 聚合不完整: %+v", first)
	}
	if _, ok := first.Vectors.(vector.NoopStore); !ok || first.Vectors.Enabled() {
		t.Fatalf("OIDC 租户必须强制使用 NoopStore, got %T", first.Vectors)
	}
	for _, dir := range []string{filepath.Join(manager.cfg.DataDir, "users"), filepath.Join(manager.cfg.DataDir, "users", uid)} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("租户目录权限 = %o, 期望 700", info.Mode().Perm())
		}
	}
}

func TestManagerIsolatesSameIDsBetweenUsers(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	alice := acquireForTest(t, manager, uuid.NewString())
	bob := acquireForTest(t, manager, uuid.NewString())
	if alice.DB == bob.DB || alice.Store == bob.Store {
		t.Fatal("不同用户不得共享 DB 或 Store")
	}
	ctx := context.Background()
	for _, item := range []struct {
		services *Services
		name     string
	}{{alice, "Alice 的库"}, {bob, "Bob 的库"}} {
		if err := item.services.Store.CreateLibrary(ctx, &domain.Library{ID: "same-library-id", Name: item.name, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if err := item.services.Settings.Set(ctx, service.KeyLLMChatModel, item.name); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		services *Services
		want     string
	}{{alice, "Alice 的库"}, {bob, "Bob 的库"}} {
		library, err := item.services.Store.GetLibrary(ctx, "same-library-id")
		if err != nil || library.Name != item.want {
			t.Fatalf("同 ID 知识库发生串库: library=%+v err=%v", library, err)
		}
		if got := item.services.Settings.Effective(ctx)[service.KeyLLMChatModel]; got != item.want {
			t.Fatalf("同 key 设置发生串库: got %q want %q", got, item.want)
		}
	}
}

func TestManagerFailureIsNotCachedAndPartialDBIsClosed(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	uid := uuid.NewString()
	var partial *sqlite.DB
	var calls atomic.Int32
	manager.open = func(path string) (*sqlite.DB, error) {
		if calls.Add(1) == 1 {
			var err error
			partial, err = sqlite.Open(path)
			if err != nil {
				return nil, err
			}
			return partial, errors.New("注入初始化失败")
		}
		return sqlite.Open(path)
	}
	if services, release, err := manager.Acquire(uid); err == nil || services != nil {
		if release != nil {
			release()
		}
		t.Fatalf("初始化失败必须返回错误且不缓存: services=%v err=%v", services, err)
	}
	if _, err := partial.Store().ListLibraries(context.Background()); err == nil {
		t.Fatal("初始化失败后必须关闭已打开的部分 DB")
	}
	first := acquireForTest(t, manager, uid)
	second := acquireForTest(t, manager, uid)
	if second != first {
		t.Fatalf("成功后的 Services 必须缓存: second=%p first=%p", second, first)
	}
	if calls.Load() != 2 {
		t.Fatalf("打开次数 = %d, 期望失败与成功各一次", calls.Load())
	}
}

func TestManagerCloseClosesEveryTenant(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	alice, releaseAlice, err := manager.Acquire(uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseAlice)
	bob, releaseBob, err := manager.Acquire(uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseBob)
	releaseAlice()
	releaseBob()
	if err := manager.Close(); err != nil {
		t.Fatalf("关闭 Manager 失败: %v", err)
	}
	for name, services := range map[string]*Services{"alice": alice, "bob": bob} {
		if _, err := services.Store.ListLibraries(context.Background()); err == nil {
			t.Errorf("Manager.Close 未关闭 %s 的 DB", name)
		}
	}
	if services, release, err := manager.Acquire(uuid.NewString()); err == nil {
		if release != nil {
			release()
		}
		t.Fatalf("Manager 关闭后不得再打开租户: services=%v", services)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("重复 Close 应安全: %v", err)
	}
}

func TestOIDCTenantsDoNotInheritEnvironmentAPIKeys(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.LLM = config.LLMConfig{
		BaseURL: "https://open.bigmodel.cn/api/paas/v4", APIKey: "server-environment-secret",
		ChatModel: "glm-4", EmbedModel: "embedding-3",
	}
	manager := NewManager(cfg)
	t.Cleanup(func() { _ = manager.Close() })
	for range 2 {
		services := acquireForTest(t, manager, uuid.NewString())
		if got := services.Settings.LLMConfig(context.Background()).APIKey; got != "" {
			t.Fatalf("OIDC 租户继承了环境 LLM Key: %q", got)
		}
		if got := services.Settings.EmbedConfig(context.Background()).APIKey; got != "" {
			t.Fatalf("OIDC 租户继承了派生 Embed Key: %q", got)
		}
	}
	if cfg.LLM.APIKey != "server-environment-secret" {
		t.Fatal("清理租户秘密不得修改全局 Config")
	}
}

func TestNoAuthServicesRetainEnvironmentAPIKeys(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t.TempDir())
	cfg.NoAuth = true
	cfg.LLM.APIKey = "local-environment-secret"
	services := NewServices(db, cfg, vector.NoopStore{})
	t.Cleanup(func() { _ = services.Close() })
	if got := services.Settings.LLMConfig(context.Background()).APIKey; got != "local-environment-secret" {
		t.Fatalf("NoAuth 必须保留环境 LLM Key: %q", got)
	}
	if got := services.Settings.EmbedConfig(context.Background()).APIKey; got != "local-environment-secret" {
		t.Fatalf("NoAuth 必须保留派生 Embed Key: %q", got)
	}
}

func TestTwoUsersSettingsAndInternalSearchUsageIsolation(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	alice := acquireForTest(t, manager, uuid.NewString())
	bob := acquireForTest(t, manager, uuid.NewString())
	ctx := context.Background()
	usageKey := "search.usage.2026-11-11"
	for _, item := range []struct {
		services *Services
		model    string
		usage    string
	}{{alice, "alice-model", "1"}, {bob, "bob-model", "7"}} {
		if err := item.services.Settings.Set(ctx, service.KeyLLMChatModel, item.model); err != nil {
			t.Fatal(err)
		}
		if err := item.services.Settings.Set(ctx, usageKey, "99"); err == nil {
			t.Fatal("公开设置入口不得写入 Agent 内部 usage key")
		}
		// Agent 仍通过 Store 记录内部用量，不经过公开设置 allowlist。
		if err := item.services.Store.PutSetting(ctx, usageKey, item.usage); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		services *Services
		model    string
		usage    string
	}{{alice, "alice-model", "1"}, {bob, "bob-model", "7"}} {
		if got := item.services.Settings.Effective(ctx)[service.KeyLLMChatModel]; got != item.model {
			t.Fatalf("双用户设置串库: got %q want %q", got, item.model)
		}
		if got, err := item.services.Store.GetSetting(ctx, usageKey); err != nil || got != item.usage {
			t.Fatalf("双用户 usage 串库: got %q want %q err=%v", got, item.usage, err)
		}
	}
}

func TestManagerSweepSkipsActiveReleaseAndReopensPersistentData(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	uid := uuid.NewString()
	services, release, err := manager.Acquire(uid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	ctx := context.Background()
	want := &domain.Library{ID: "persistent-library", Name: "淘汰后仍保留", CreatedAt: time.Now()}
	if err := services.Store.CreateLibrary(ctx, want); err != nil {
		t.Fatal(err)
	}

	manager.mu.Lock()
	cached := manager.entries[uid]
	if cached == nil {
		manager.mu.Unlock()
		t.Fatal("Acquire 后必须存在缓存条目")
	}
	cached.lastUsed = time.Now().Add(-31 * time.Minute)
	manager.mu.Unlock()
	manager.sweepIdle(time.Now())

	manager.mu.Lock()
	cached = manager.entries[uid]
	refs := 0
	if cached != nil {
		refs = cached.refs
	}
	manager.mu.Unlock()
	if cached == nil || refs != 1 {
		t.Fatalf("活动引用不得被淘汰: entry=%p refs=%d", cached, refs)
	}
	if got, err := services.Store.GetLibrary(ctx, want.ID); err != nil || got.Name != want.Name {
		t.Fatalf("活动引用的 DB 不得被 sweep 关闭: library=%+v err=%v", got, err)
	}

	release()
	manager.mu.Lock()
	cached = manager.entries[uid]
	if cached == nil {
		manager.mu.Unlock()
		t.Fatal("Release 后尚未过期的条目应保留")
	}
	cached.lastUsed = time.Now().Add(-31 * time.Minute)
	manager.mu.Unlock()
	manager.sweepIdle(time.Now())

	manager.mu.Lock()
	_, exists := manager.entries[uid]
	manager.mu.Unlock()
	if exists {
		t.Fatal("Release 且空闲超过 30 分钟的缓存条目必须删除")
	}
	if _, err := services.Store.ListLibraries(ctx); err == nil {
		t.Fatal("过期淘汰必须关闭 Services")
	}
	path, err := manager.DBPath(uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("淘汰不得删除 SQLite 文件: %v", err)
	}

	reopened, releaseReopened, err := manager.Acquire(uid)
	if err != nil {
		t.Fatalf("淘汰后重新 Acquire 失败: %v", err)
	}
	t.Cleanup(releaseReopened)
	got, err := reopened.Store.GetLibrary(ctx, want.ID)
	if err != nil || got.Name != want.Name {
		t.Fatalf("重开后 SQLite 数据必须保留: library=%+v err=%v", got, err)
	}
}

func TestManagerReleaseIsIdempotent(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	uid := uuid.NewString()
	_, release, err := manager.Acquire(uid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	release()
	release()

	manager.mu.Lock()
	cached := manager.entries[uid]
	refs := 0
	if cached != nil {
		refs = cached.refs
	}
	manager.mu.Unlock()
	if cached == nil || refs != 0 {
		t.Fatalf("重复 release 不得产生负引用: entry=%p refs=%d", cached, refs)
	}
}

func TestManagerConcurrentAcquireAndSweep(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	uid := uuid.NewString()
	ctx := context.Background()

	for range 100 {
		start := make(chan struct{})
		errs := make(chan error, 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			services, release, err := manager.Acquire(uid)
			if err != nil {
				errs <- err
				return
			}
			defer release()
			if _, err := services.Store.ListLibraries(ctx); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			manager.mu.Lock()
			if cached := manager.entries[uid]; cached != nil {
				cached.lastUsed = time.Now().Add(-31 * time.Minute)
			}
			manager.mu.Unlock()
			manager.sweepIdle(time.Now())
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("并发 Acquire/Sweep 不得关闭活动引用的 DB: %v", err)
		}
	}
}

func TestManagerCloseWaitsForReferencesAndStopsJanitor(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	uid := uuid.NewString()
	services, release, err := manager.Acquire(uid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)

	closeStarted := make(chan struct{})
	closeDone := make(chan error, 1)
	go func() {
		close(closeStarted)
		closeDone <- manager.Close()
	}()
	<-closeStarted
	select {
	case err := <-closeDone:
		t.Fatalf("存在活动引用时 Close 不得提前完成: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	release()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close 失败: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Release 后 Close 仍未完成")
	}
	if _, err := services.Store.ListLibraries(context.Background()); err == nil {
		t.Fatal("Close 完成后必须关闭已缓存 Services")
	}
	if acquired, releaseAfterClose, err := manager.Acquire(uid); err == nil {
		if releaseAfterClose != nil {
			releaseAfterClose()
		}
		t.Fatalf("Close 后 Acquire 必须拒绝: services=%v", acquired)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("重复 Close 必须幂等: %v", err)
	}
	select {
	case <-manager.janitorDone:
	default:
		t.Fatal("Close 完成后 janitorDone 必须关闭")
	}
}

type failingCloseVectorStore struct {
	vector.NoopStore
	closeCalls atomic.Int32
}

func (f *failingCloseVectorStore) Close() error {
	f.closeCalls.Add(1)
	return errors.New("注入 VectorStore.Close 失败")
}

func TestManagerSweepEvictsPartiallyClosedEntrySoAcquireReopens(t *testing.T) {
	manager := NewManager(testConfig(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	uid := uuid.NewString()
	services, release, err := manager.Acquire(uid)
	if err != nil {
		t.Fatal(err)
	}
	vectors := &failingCloseVectorStore{}
	services.Vectors = vectors
	release()

	manager.mu.Lock()
	cached := manager.entries[uid]
	if cached == nil {
		manager.mu.Unlock()
		t.Fatal("Release 后必须保留缓存条目")
	}
	cached.lastUsed = time.Now().Add(-31 * time.Minute)
	manager.mu.Unlock()
	manager.sweepIdle(time.Now())
	if got := vectors.closeCalls.Load(); got != 1 {
		t.Fatalf("VectorStore.Close 调用次数 = %d, 期望 1", got)
	}

	reopened, releaseAgain, err := manager.Acquire(uid)
	if err != nil {
		t.Fatalf("部分关闭后的条目必须允许重新打开: %v", err)
	}
	defer releaseAgain()
	if reopened == services {
		t.Fatal("部分关闭后不得继续复用旧 Services")
	}
	if _, err := reopened.Store.ListLibraries(context.Background()); err != nil {
		t.Fatalf("重新 Acquire 的 DB 必须可用: %v", err)
	}
}

func upperASCII(value string) string {
	bytes := []byte(value)
	for i, char := range bytes {
		if char >= 'a' && char <= 'f' {
			bytes[i] = char - ('a' - 'A')
		}
	}
	return string(bytes)
}
