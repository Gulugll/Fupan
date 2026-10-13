-- 复磐 FuPan 初始 schema（v0.5 MVP）
-- 硬约束：
--   1) chunks.content 原文不可变：永不 UPDATE，修订一律 INSERT 新行（version+1）
--   2) cards.source_chunk_id 溯源必填：NOT NULL 外键
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS libraries (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS documents (
    id         TEXT PRIMARY KEY,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    format     TEXT NOT NULL DEFAULT 'md',        -- 'md' | 'txt'
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_documents_library ON documents(library_id);

CREATE TABLE IF NOT EXISTS chunks (
    id          TEXT PRIMARY KEY,
    doc_id      TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    parent_id   TEXT REFERENCES chunks(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,                    -- 'parent' | 'child'
    title_path  TEXT NOT NULL DEFAULT '',
    seq         INTEGER NOT NULL DEFAULT 0,
    content     TEXT NOT NULL,                    -- 不可变：只 INSERT
    char_start  INTEGER NOT NULL DEFAULT 0,
    char_end    INTEGER NOT NULL DEFAULT 0,
    author      TEXT NOT NULL DEFAULT 'user',     -- 'user' | 'agent'
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chunks_doc ON chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_chunks_parent ON chunks(parent_id);

CREATE TABLE IF NOT EXISTS cards (
    id              TEXT PRIMARY KEY,
    library_id      TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    front           TEXT NOT NULL,
    back            TEXT NOT NULL,
    type            TEXT NOT NULL DEFAULT 'qa',   -- MVP 仅问答卡
    source_chunk_id TEXT NOT NULL REFERENCES chunks(id),
    source_version  INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cards_library ON cards(library_id);
CREATE INDEX IF NOT EXISTS idx_cards_chunk ON cards(source_chunk_id);

CREATE TABLE IF NOT EXISTS schedules (
    card_id         TEXT PRIMARY KEY REFERENCES cards(id) ON DELETE CASCADE,
    state           TEXT NOT NULL DEFAULT 'new',  -- 'new' | 'learning' | 'review' | 'relearning'
    due             TEXT NOT NULL,
    stability       REAL NOT NULL DEFAULT 0,
    difficulty      REAL NOT NULL DEFAULT 0,
    elapsed_days    INTEGER NOT NULL DEFAULT 0,
    scheduled_days  INTEGER NOT NULL DEFAULT 0,
    reps            INTEGER NOT NULL DEFAULT 0,
    lapses          INTEGER NOT NULL DEFAULT 0,
    introduced_date TEXT NOT NULL DEFAULT ''      -- 首次出现于复习队列的日期（YYYY-MM-DD），计每日新卡数
);

CREATE INDEX IF NOT EXISTS idx_schedules_due ON schedules(due);

CREATE TABLE IF NOT EXISTS review_logs (
    id          TEXT PRIMARY KEY,
    card_id     TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    rating      INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 4),
    reviewed_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_review_logs_card ON review_logs(card_id);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
