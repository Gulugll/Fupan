package chunker

import (
	"strings"
)

// sentence 带原文 rune 偏移的句子（供字符区间溯源）。
type sentence struct {
	text       string
	start, end int // rune 下标，[start, end)
}

// splitSentencesWithOffsets 按中文句读（。！？；）与换行断句，保留标点。
func splitSentencesWithOffsets(text string) []sentence {
	runes := []rune(text)
	var out []sentence
	start := 0
	flush := func(end int) {
		seg := strings.TrimSpace(string(runes[start:end]))
		if seg != "" {
			// 修正 trim 后的实际起点（段首空白不计入内容）。
			trimStart := start
			for trimStart < end && isSpace(runes[trimStart]) {
				trimStart++
			}
			out = append(out, sentence{text: string(runes[trimStart:end]), start: trimStart, end: end})
		}
		start = end
	}
	for i, r := range runes {
		if r == '。' || r == '！' || r == '？' || r == '；' || r == '\n' {
			flush(i + 1)
		}
	}
	flush(len(runes))
	return out
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }

// takeSentencesUntil 从 sentences[idx:] 取 token 数不超过 limit 的最大前缀，
// 返回取走的句子数。至少返回 1 句（避免死循环）。
func takeSentencesUntil(sentences []sentence, idx, limit int) int {
	tokens := 0
	n := 0
	for i := idx; i < len(sentences); i++ {
		t := CountTokens(sentences[i].text)
		if n > 0 && tokens+t > limit {
			break
		}
		tokens += t
		n++
	}
	return n
}

// overlapSentenceCount 返回从尾部往前累计约 overlapRatio 比例 token 的句子数。
func overlapSentenceCount(sentences []sentence, from, to, totalTokens int) int {
	target := int(float64(totalTokens) * OverlapRatio)
	if target <= 0 {
		return 0
	}
	tokens := 0
	n := 0
	for i := to - 1; i >= from; i-- {
		tokens += CountTokens(sentences[i].text)
		n++
		if tokens >= target {
			break
		}
	}
	// 重叠句不能吞掉整个窗口。
	if n >= to-from {
		return to - from - 1
	}
	return n
}
