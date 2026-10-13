package chunker

import (
	"fmt"
	"strings"
	"testing"
)

// 构造 N 句带编号的纯文本（每句约 35 字 ≈ 23 token）。
func makeLongText(sentences int) string {
	var b strings.Builder
	for i := 0; i < sentences; i++ {
		b.WriteString(fmt.Sprintf("这是第%d句话，用于测试滑动窗口切分的句子边界处理逻辑，句子长度固定且包含中文标点。", i))
	}
	return b.String()
}

func TestFixedSlidingWindowBasics(t *testing.T) {
	text := makeLongText(60) // 60 句 × ~34 字 ≈ 2040 字 ≈ 1360 token → 至少 2 窗
	c := NewFixedChunker()
	units := c.Chunk(text)

	if len(units) < 2 {
		t.Fatalf("窗口数 = %d, 期望 >= 2", len(units))
	}

	// 每个窗口（除最后一块）应在目标范围附近：不超过 max 太多，也不碎。
	for i, u := range units {
		n := CountTokens(u.Content)
		if n > WindowMaxTokens+100 {
			t.Fatalf("unit[%d] 超过窗口上限: %d token", i, n)
		}
		if i < len(units)-1 && n < WindowMinTokens-150 {
			t.Fatalf("unit[%d] 过碎: %d token", i, n)
		}
		// 句子边界：每块必须以中文句读结尾（不截断句中）。
		rr := []rune(u.Content)
		last := rr[len(rr)-1]
		if last != '。' && last != '！' && last != '？' && last != '；' {
			t.Fatalf("unit[%d] 未在句子边界结束: 尾字符 %q", i, last)
		}
	}
}

func TestFixedOverlapBetweenWindows(t *testing.T) {
	text := makeLongText(60)
	c := NewFixedChunker()
	units := c.Chunk(text)

	// 相邻窗口应有约 15% 重叠：后一窗口开头应能在前一窗口尾部找到。
	for i := 1; i < len(units); i++ {
		prev := units[i-1].Content
		cur := units[i].Content
		head := firstSentence(cur)
		if head == "" {
			t.Fatalf("unit[%d] 为空", i)
		}
		if !strings.Contains(prev, head) {
			t.Fatalf("unit[%d] 与 unit[%d] 无重叠", i, i-1)
		}
	}
}

func TestFixedRespectsSentenceBoundary(t *testing.T) {
	// 不按空格断句：含英文与空格混排的中文文本，切点必须仍在中文句读。
	text := "机器学习是一门学科，涉及统计学与优化理论。Deep learning is a subset of ML. 神经网络由多层组成；每层有若干神经元！训练目标是最小化损失。"
	c := NewFixedChunker()
	units := c.Chunk(text)

	joined := ""
	for _, u := range units {
		joined += u.Content
	}
	// 内容不得丢失（拼接后覆盖所有句子）。
	for _, frag := range []string{"机器学习是一门学科", "神经网络由多层组成；", "训练目标是最小化损失。"} {
		if !strings.Contains(joined, frag) {
			t.Fatalf("切分丢失内容: %q", frag)
		}
	}
}

func TestFixedParentIsWholeDocument(t *testing.T) {
	text := makeLongText(60)
	c := NewFixedChunker()
	units := c.Chunk(text)

	for i, u := range units {
		if u.Parent != text {
			t.Fatalf("unit[%d] 父块应为整篇文档", i)
		}
		if u.TitlePath != "" {
			t.Fatalf("TXT 无标题路径, got %q", u.TitlePath)
		}
	}
}

func TestFixedShortTextSingleChunk(t *testing.T) {
	text := "只有一句话。"
	c := NewFixedChunker()
	units := c.Chunk(text)
	if len(units) != 1 || units[0].Content != text {
		t.Fatalf("短文本应原样成块: %+v", units)
	}
}

func firstSentence(s string) string {
	for i, r := range s {
		if r == '。' || r == '！' || r == '？' || r == '；' {
			return s[:i+len(string(r))]
		}
	}
	return ""
}
