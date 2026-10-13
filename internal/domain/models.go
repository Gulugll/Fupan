// Package domain 定义复磐的核心实体（纯数据结构，不依赖任何外部库）。
// 实体关系：Library 1—N Document 1—N Chunk（父子双粒度）；Chunk 1—N Card；Card 1—1 Schedule；Card 1—N ReviewLog。
package domain

import "time"

// Library 知识库（多库支持，设计文档 10.1 决策 #7）。
type Library struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// Document 已导入文档。
type Document struct {
	ID        string    `json:"id"`
	LibraryID string    `json:"library_id"`
	Title     string    `json:"title"`
	Format    string    `json:"format"` // "md" | "txt"
	CreatedAt time.Time `json:"created_at"`
}

// ChunkKind 区分父块（上下文单元，存 SQLite）与子块（检索单元，进向量库）。
type ChunkKind string

const (
	ChunkParent ChunkKind = "parent"
	ChunkChild  ChunkKind = "child"
)

// ChunkAuthor 标记块来源（设计文档 6.3 可追溯原则）。
type ChunkAuthor string

const (
	AuthorUser  ChunkAuthor = "user"
	AuthorAgent ChunkAuthor = "agent"
)

// Chunk 切片。content 不可变：修订一律插入 version+1 的新行。
type Chunk struct {
	ID        string      `json:"id"`
	DocID     string      `json:"doc_id"`
	ParentID  string      `json:"parent_id"`
	Kind      ChunkKind   `json:"kind"`
	TitlePath string      `json:"title_path"`
	Seq       int         `json:"seq"`
	Content   string      `json:"content"`
	CharStart int         `json:"char_start"`
	CharEnd   int         `json:"char_end"`
	Author    ChunkAuthor `json:"author"`
	Version   int         `json:"version"`
	CreatedAt time.Time   `json:"created_at"`
}

// CardType MVP 仅问答卡（设计文档 10.2）。
type CardType string

const CardQA CardType = "qa"

// Card 卡片。source_chunk_id 溯源必填。
type Card struct {
	ID            string    `json:"id"`
	LibraryID     string    `json:"library_id"`
	Front         string    `json:"front"`
	Back          string    `json:"back"`
	Type          CardType  `json:"type"`
	SourceChunkID string    `json:"source_chunk_id"`
	SourceVersion int       `json:"source_version"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// ScheduleState FSRS 卡片状态。
type ScheduleState string

const (
	StateNew        ScheduleState = "new"
	StateLearning   ScheduleState = "learning"
	StateReview     ScheduleState = "review"
	StateRelearning ScheduleState = "relearning"
)

// Schedule 调度状态（对应 go-fsrs 的 Card）。
type Schedule struct {
	CardID         string        `json:"card_id"`
	State          ScheduleState `json:"state"`
	Due            time.Time     `json:"due"`
	Stability      float64       `json:"stability"`
	Difficulty     float64       `json:"difficulty"`
	ElapsedDays    int           `json:"elapsed_days"`
	ScheduledDays  int           `json:"scheduled_days"`
	Reps           int           `json:"reps"`
	Lapses         int           `json:"lapses"`
	IntroducedDate string        `json:"introduced_date"`
}

// ReviewLog 复习记录。Rating 1–4 与 UI 等级一一对应（设计文档 3 模式 A）。
type ReviewLog struct {
	ID         string    `json:"id"`
	CardID     string    `json:"card_id"`
	Rating     int       `json:"rating"` // 1=again 2=hard 3=good 4=easy
	ReviewedAt time.Time `json:"reviewed_at"`
}
