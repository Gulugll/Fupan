package sqlite

import (
	"testing"

	"github.com/Gulugll/fupan/internal/repo"
)

// 测试断言点：
//  1. 打开内存库并跑迁移成功；
//  2. 外键约束生效（cards.source_chunk_id 指向不存在的 chunk 必须被拒绝）；
//  3. 实现满足 repo.Store 接口。

func TestOpenAndMigrate(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	defer d.Close()

	// 迁移后所有表应存在。
	for _, table := range []string{"libraries", "documents", "chunks", "cards", "schedules", "review_logs", "settings"} {
		var name string
		err := d.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("表 %s 不存在: %v", table, err)
		}
	}
}

func TestSourceChunkForeignKeyEnforced(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	defer d.Close()

	// 建库后直接插入指向不存在 chunk 的卡片，必须报外键错误（溯源必填的数据库层兜底）。
	_, err = d.db.Exec(`INSERT INTO libraries(id,name,created_at) VALUES('l1','测试库','2026-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.db.Exec(`INSERT INTO cards(id,library_id,front,back,source_chunk_id,source_version,created_at)
		VALUES('c1','l1','问','答','ghost-chunk',1,'2026-01-01')`)
	if err == nil {
		t.Fatal("source_chunk_id 外键未生效：指向不存在的 chunk 却插入成功")
	}
}

func TestImplementsRepoStore(t *testing.T) {
	var _ repo.Store = (*store)(nil)
}
