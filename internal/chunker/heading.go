package chunker

import (
	"strings"
)

// HeadingChunker Markdown 标题树切分（默认策略，设计文档 4.1.1）。
// 规则：H1/H2/H3 边界切块，chunk 携带完整标题路径；代码块/表格原子保护；
// <100 token 相邻同父小节合并；>1000 token 小节按段落+overlap 二次切分。
type HeadingChunker struct{}

func NewHeadingChunker() *HeadingChunker { return &HeadingChunker{} }

func (c *HeadingChunker) Name() string { return "heading" }

// section 一个标题树小节（用行切片存储，避免 strings.Builder 拷贝问题）。
type section struct {
	path       []string // [H1, H2, H3] 中已确定的部分
	titlePath  string
	lines      []string
	start, end int // rune 区间（含标题行）
}

func (s *section) text() string {
	return strings.TrimSpace(strings.Join(s.lines, "\n"))
}

func (c *HeadingChunker) Chunk(content string) []Unit {
	runes := []rune(content)
	lines := strings.Split(content, "\n")

	// 预计算每行起始 rune 偏移。
	lineOffsets := make([]int, len(lines)+1)
	off := 0
	for i, ln := range lines {
		lineOffsets[i] = off
		off += len([]rune(ln)) + 1 // +1 换行
	}
	lineOffsets[len(lines)] = off

	var sections []section
	var path [3]string
	inFence := false
	fenceMark := ""
	cur := -1 // 当前 section 下标

	appendLine := func(i int) {
		if cur >= 0 {
			sections[cur].lines = append(sections[cur].lines, lines[i])
			sections[cur].end = lineOffsets[i+1]
		}
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 代码块围栏：进出状态（原子保护的核心——围栏内的 # 不是标题）。
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				inFence, fenceMark = true, trimmed[:3]
			} else if strings.HasPrefix(trimmed, fenceMark) {
				inFence = false
			}
			appendLine(i)
			continue
		}
		if inFence {
			appendLine(i)
			continue
		}

		// 标题行（仅 1–3 级参与切分与路径）。
		if level, title, ok := parseHeading(trimmed); ok {
			if level <= 3 {
				path[level-1] = title
				for j := level; j < 3; j++ {
					path[j] = ""
				}
				sections = append(sections, section{
					path:      []string{path[0], path[1], path[2]},
					titlePath: joinPath(path),
					start:     lineOffsets[i],
				})
				cur = len(sections) - 1
				appendLine(i)
				continue
			}
		}

		// 普通行：无任何标题前（文档开头散文本）先开一个匿名 section。
		if cur < 0 {
			sections = append(sections, section{path: []string{"", "", ""}, start: lineOffsets[i]})
			cur = len(sections) - 1
		}
		appendLine(i)
	}

	// 清洗：去掉空 section；合并超短小节；二次切分超大小节。
	return c.buildUnits(runes, sections)
}

func parseHeading(line string) (level int, title string, ok bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i >= len(line) || line[i] != ' ' {
		return 0, "", false
	}
	return i, strings.TrimSpace(line[i:]), true
}

func joinPath(path [3]string) string {
	var parts []string
	for _, p := range path {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " > ")
}

func parentKey(s *section) string {
	// 父路径 = 去掉最后一级非空标题。
	p := s.path
	for i := 2; i >= 0; i-- {
		if p[i] != "" {
			return strings.Join(p[:i], "\x00")
		}
	}
	return ""
}

// headingOnly 判断小节是否只有标题行（无正文）——纯标题不产出 chunk。
func headingOnly(s *section) bool {
	for _, ln := range s.lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if _, _, ok := parseHeading(t); ok {
			continue
		}
		return false
	}
	return true
}

func (c *HeadingChunker) buildUnits(runes []rune, sections []section) []Unit {
	// 1. 丢弃无内容或纯标题（无正文）的 section。
	var kept []section
	for _, s := range sections {
		if headingOnly(&s) {
			continue
		}
		kept = append(kept, s)
	}

	// 2. 超短小节合并：与前一个 section 同父路径且 < MergeMinTokens 时并入前者。
	var merged []section
	for i := range kept {
		s := kept[i]
		if len(merged) > 0 && CountTokens(s.text()) < MergeMinTokens &&
			parentKey(&s) == parentKey(&merged[len(merged)-1]) {
			prev := &merged[len(merged)-1]
			prev.lines = append(prev.lines, "")
			prev.lines = append(prev.lines, s.lines...)
			prev.end = s.end
			continue
		}
		merged = append(merged, s)
	}

	// 3. 产出子块（超大小节二次切分）。
	var units []Unit
	for _, s := range merged {
		text := s.text()
		if CountTokens(text) > OversizeTokens {
			for _, u := range splitOversize(text, s.titlePath, s.start) {
				units = append(units, u)
			}
			continue
		}
		start := s.start
		// 跳过内容前导空白，让区间精确指向正文。
		adj := 0
		r := []rune(text)
		for adj < len(r) && isSpace(r[adj]) {
			adj++
		}
		units = append(units, Unit{
			TitlePath: s.titlePath,
			Content:   text,
			Parent:    text,
			CharStart: start + adj,
			CharEnd:   start + len(r),
			Seq:       len(units),
		})
	}
	_ = runes
	return units
}

// splitOversize 超大小节二次切分：按句子滑窗（500–800 token、overlap 15%），
// 返回的每个 Unit 共享同一父块全文与标题路径。
func splitOversize(text, titlePath string, sectionStart int) []Unit {
	sents := splitSentencesWithOffsets(text)
	// 病理情形兜底：单句超阈值时按次级标点（，、：）再断，仍超则硬切。
	var fine []sentence
	for _, s := range sents {
		if CountTokens(s.text) <= OversizeTokens {
			fine = append(fine, s)
			continue
		}
		fine = append(fine, splitLongSentence(s)...)
	}
	var units []Unit
	i := 0
	for i < len(fine) {
		n := takeSentencesUntil(fine, i, WindowMaxTokens)
		end := i + n
		w := fine[i:end]
		total := 0
		for _, s := range w {
			total += CountTokens(s.text)
		}
		ov := overlapSentenceCount(fine, i, end, total)
		content := joinSentences(w)
		units = append(units, Unit{
			TitlePath: titlePath,
			Content:   content,
			Parent:    text,
			CharStart: sectionStart + w[0].start,
			CharEnd:   sectionStart + w[len(w)-1].end,
			Seq:       len(units),
		})
		i = end - ov
		if ov == 0 {
			i = end
		}
	}
	return units
}

// splitLongSentence 超长单句兜底：先按次级标点（，、：）断，
// 仍超 OversizeTokens×1.5 则按约 500 token 硬切（极端情形，牺牲句子边界保上限）。
func splitLongSentence(s sentence) []sentence {
	runes := []rune(s.text)
	var out []sentence
	segStart := 0
	flush := func(end int) {
		if end <= segStart {
			return
		}
		out = append(out, sentence{text: string(runes[segStart:end]), start: s.start + segStart, end: s.start + end})
	}
	for i, r := range runes {
		if r == '，' || r == '、' || r == '：' {
			flush(i + 1)
			segStart = i + 1
		}
	}
	flush(len(runes))

	// 仍有过长片段 → 硬切。
	var final []sentence
	for _, seg := range out {
		if CountTokens(seg.text) <= int(OversizeTokens*1.5) {
			final = append(final, seg)
			continue
		}
		rr := []rune(seg.text)
		step := 750 // ≈500 token
		for st := 0; st < len(rr); st += step {
			en := st + step
			if en > len(rr) {
				en = len(rr)
			}
			final = append(final, sentence{text: string(rr[st:en]), start: seg.start + st, end: seg.start + en})
		}
	}
	return final
}

func joinSentences(ss []sentence) string {
	var b strings.Builder
	for i, s := range ss {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s.text)
	}
	return b.String()
}
