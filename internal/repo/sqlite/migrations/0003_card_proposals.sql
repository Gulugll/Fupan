-- V0.7 agent 卡片写回提案（HITL）：agent 只产提案，用户确认后由 service 落库。
CREATE TABLE IF NOT EXISTS card_proposals (
    id         TEXT PRIMARY KEY,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    card_id    TEXT REFERENCES cards(id) ON DELETE CASCADE,  -- edit 提案非空；new 为空
    kind       TEXT NOT NULL,                                -- 'edit' | 'new'
    payload    TEXT NOT NULL,                                -- JSON: {front,back,category,source_chunk_id?}
    status     TEXT NOT NULL DEFAULT 'pending',              -- 'pending' | 'applied' | 'rejected'
    rationale  TEXT NOT NULL DEFAULT '',                     -- agent 给出的理由/依据
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_card_proposals_library ON card_proposals(library_id, status);
