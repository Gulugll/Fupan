// Package domain 定义复磐的核心实体（纯数据结构，不依赖任何外部库）。
// 实体关系：Library 1—N Document 1—N Chunk（父子双粒度）；Chunk 1—N Card；Card 1—1 Schedule；Card 1—N ReviewLog。
package domain

import "time"

// Library 知识库（多库支持，设计文档 10.1 决策 #7）。
type Library struct {
	ID          string
	Name        string
	Description string
	CreatedAt   time.Time
}

// Document 已导入文档。
type Document struct {
	ID        string
	LibraryID string
	Title     string
	Format    string // "md" | "txt"
	CreatedAt time.Time
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
	ID        string
	DocID     string
	ParentID  string // 子块指向父块；父块为空
	Kind      ChunkKind
	TitlePath string // 如 "机器学习 > 2.3 损失函数"
	Seq       int    // 块序号
	Content   string
	CharStart int
	CharEnd   int
	Author    ChunkAuthor
	Version   int
	CreatedAt time.Time
}

// CardType MVP 仅问答卡（设计文档 10.2）。
type CardType string

const CardQA CardType = "qa"

// Card 卡片。source_chunk_id 溯源必填。
type Card struct {
	ID            string
	LibraryID     string
	Front         string
	Back          string
	Type          CardType
	SourceChunkID string // NOT NULL，溯源锚点
	SourceVersion int
	Status        string // "active"
	CreatedAt     time.Time
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
	CardID         string
	State          ScheduleState
	Due            time.Time
	Stability      float64
	Difficulty     float64
	ElapsedDays    int
	ScheduledDays  int
	Reps           int
	Lapses         int
	IntroducedDate string // "2006-01-02"，计每日新卡上限
}

// ReviewLog 复习记录。Rating 1–4 与 UI 等级一一对应（设计文档 3 模式 A）。
type ReviewLog struct {
	ID         string
	CardID     string
	Rating     int // 1=again 2=hard 3=good 4=easy
	ReviewedAt time.Time
}
