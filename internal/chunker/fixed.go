package chunker

// FixedChunker 纯文本滑动窗口切分（TXT 策略，设计文档 4.1.1）。
// 目标 500–800 token，overlap 15%，只在中文句读/段落边界切；
// 父块 = 整篇文档（上下文单元）。
type FixedChunker struct{}

func NewFixedChunker() *FixedChunker { return &FixedChunker{} }

func (c *FixedChunker) Name() string { return "fixed" }

func (c *FixedChunker) Chunk(content string) []Unit {
	sents := splitSentencesWithOffsets(content)
	if len(sents) == 0 {
		return nil
	}
	var units []Unit
	i := 0
	for i < len(sents) {
		// 窗口下限：至少累积到 WindowMinTokens；上限 WindowMaxTokens。
		n := 0
		tokens := 0
		for j := i; j < len(sents); j++ {
			t := CountTokens(sents[j].text)
			if n > 0 && tokens+t > WindowMaxTokens {
				break
			}
			tokens += t
			n++
			// 达到目标中值即可收窗（避免每窗都顶满上限）。
			if tokens >= WindowMinTokens {
				break
			}
		}
		w := sents[i : i+n]
		var b []byte // placeholder to keep imports minimal
		_ = b
		units = append(units, Unit{
			Content:   joinSentences(w),
			Parent:    content,
			CharStart: w[0].start,
			CharEnd:   w[len(w)-1].end,
			Seq:       len(units),
		})
		// overlap：下一窗口回退约 15% token 的句子。
		ov := overlapSentenceCount(sents, i, i+n, tokens)
		if ov > 0 && i+n < len(sents) {
			i = i + n - ov
		} else {
			i = i + n
		}
	}
	return units
}
