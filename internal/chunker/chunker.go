// Package chunker 实现切片（硬约束：切片与嵌入解耦，策略为接口）。
//
// 双粒度输出（设计文档 4.1.2）：
//   - 子块（检索单元）：进向量库
//   - 父块（上下文单元）：存 SQLite，不进向量库；每个 Unit 携带其父块全文
//
// 长度换算：MVP 用近似 token 估算（中文场景 tokens ≈ 字符数 × 2/3，
// 依据设计文档 4.1.1「500–800 token ≈ 中文 300–500 字」）。后续可替换为真实 tokenizer。
package chunker

import "strings"

// Unit 切片结果：一个检索单元（子块）及其所属父块全文。
type Unit struct {
	TitlePath string // 标题路径，如 "机器学习 > 2.3 损失函数"；TXT 为空
	Content   string // 子块内容
	Parent    string // 父块全文（完整小节/整篇 TXT）
	CharStart int    // 子块在原文中的字符区间（rune 下标）
	CharEnd   int
	Seq       int // 全文档内子块序号，从 0 递增
}

// Chunker 切片策略接口。本版实现 heading 与 fixed 两个（设计文档 4.1.5）。
type Chunker interface {
	Name() string
	Chunk(content string) []Unit
}

// 参数默认值（设计文档 4.1.1 / 10.2）。
const (
	WindowMinTokens = 500  // 滑窗目标下限
	WindowMaxTokens = 800  // 滑窗目标上限
	OversizeTokens  = 1000 // 超过则二次切分
	MergeMinTokens  = 100  // 小于则与相邻小节合并
	OverlapRatio    = 0.15 // 滑窗重叠比例
)

// CountTokens 近似 token 数：中文 1 字 ≈ 1.5 token（即 tokens ≈ 字数 × 2/3）。
// 对 ASCII 密集文本（代码/英文）按 4 字符 ≈ 1 token 估算后取较大值，兼顾混合内容。
func CountTokens(s string) int {
	runes := []rune(s)
	if len(runes) == 0 {
		return 0
	}
	ascii := 0
	for _, r := range runes {
		if r < 128 {
			ascii++
		}
	}
	cjk := len(runes) - ascii
	// 中文按 2/3，ASCII 按 1/4。
	tokens := (cjk*2+2)/3 + (ascii+3)/4
	return tokens
}

// splitSentences 中文按 。！？；断句（设计文档 4.1.1：不按空格），
// 保留标点在句尾；换行视为段落边界单独成句。
func splitSentences(text string) []string {
	var out []string
	var buf strings.Builder
	flush := func() {
		if s := strings.TrimSpace(buf.String()); s != "" {
			out = append(out, s)
		}
		buf.Reset()
	}
	for _, r := range text {
		buf.WriteRune(r)
		switch r {
		case '。', '！', '？', '；', '\n':
			flush()
		}
	}
	flush()
	return out
}
