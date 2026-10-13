package chunker

import (
	"strings"
	"testing"
)

// fixture：覆盖 H1/H2/H3、代码块内 # 不误切、表格原子、超短小节。
// （围栏用拼接构造，避免与 Go 原始字符串字面量冲突。）
var fence = "```"

var headingFixture = "# 机器学习笔记\n\n" +
	"引言段落，介绍本笔记的目的与范围，这里是一些铺垫文字。\n\n" +
	"## 2.3 损失函数\n\n" +
	"损失函数衡量预测值与真实值的差异。\n\n" +
	"### 2.3.1 均方误差\n\n" +
	"均方误差是最常用的回归损失。\n\n" +
	fence + "python\n" +
	"# 这行注释里有 ## 也不该被当成标题切分\n" +
	"def mse(y, y_pred):\n" +
	"    return ((y - y_pred) ** 2).mean()\n" +
	fence + "\n\n" +
	"上面的代码块必须整体保留。\n\n" +
	"### 2.3.2 交叉熵\n\n" +
	"交叉熵用于分类任务。\n\n" +
	"| 损失 | 任务 |\n" +
	"| ---- | ---- |\n" +
	"| MSE  | 回归 |\n" +
	"| CE   | 分类 |\n\n" +
	"表格也必须整体保留。\n\n" +
	"## 第四章 过拟合\n\n" +
	"过拟合指模型在训练集表现好但泛化差。\n"

func TestHeadingBasicStructure(t *testing.T) {
	c := NewHeadingChunker()
	units := c.Chunk(headingFixture)

	if len(units) == 0 {
		t.Fatal("切分结果为空")
	}
	// 标题树切分应至少产生：2.3 损失函数、2.3.1、2.3.2、第四章 等 4+ 个子块。
	if len(units) < 4 {
		t.Fatalf("子块数 = %d, 期望 >= 4", len(units))
	}

	// 标题路径必须携带完整层级。
	found := false
	for _, u := range units {
		if strings.Contains(u.TitlePath, "2.3 损失函数") && strings.Contains(u.TitlePath, "2.3.1 均方误差") &&
			strings.HasPrefix(u.TitlePath, "机器学习笔记") {
			found = true
		}
	}
	if !found {
		t.Fatal("未找到携带完整标题路径（H1 > H2 > H3）的子块")
	}

	// Seq 必须从 0 连续递增。
	for i, u := range units {
		if u.Seq != i {
			t.Fatalf("Seq[%d] = %d, 期望 %d", i, u.Seq, i)
		}
	}

	// 字符区间必须能回定位到原文（CharStart/CharEnd 是 rune 下标）。
	runes := []rune(headingFixture)
	for i, u := range units {
		if u.CharStart < 0 || u.CharEnd > len(runes) || u.CharStart >= u.CharEnd {
			t.Fatalf("unit[%d] 字符区间非法: [%d,%d)", i, u.CharStart, u.CharEnd)
		}
		if !strings.Contains(string(runes[u.CharStart:u.CharEnd]), firstLine(u.Content)) {
			t.Fatalf("unit[%d] 区间内容与子块内容不对应", i)
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func TestHeadingAtomicProtection(t *testing.T) {
	c := NewHeadingChunker()
	units := c.Chunk(headingFixture)

	// 代码块不得从中间切开：包含 def mse 的子块必须同时包含围栏开头、注释行与函数体结尾。
	var codeUnit *Unit
	for i := range units {
		if strings.Contains(units[i].Content, "def mse") {
			codeUnit = &units[i]
		}
	}
	if codeUnit == nil {
		t.Fatal("未找到包含代码块的子块")
	}
	for _, frag := range []string{"```python", "# 这行注释里有 ## 也不该被当成标题切分", "return ((y - y_pred) ** 2).mean()", "```"} {
		if !strings.Contains(codeUnit.Content, frag) {
			t.Fatalf("代码块被切开，缺少片段 %q", frag)
		}
	}

	// 表格不得切开：包含 | MSE 的子块必须包含表头和表尾。
	var tableUnit *Unit
	for i := range units {
		if strings.Contains(units[i].Content, "| MSE") {
			tableUnit = &units[i]
		}
	}
	if tableUnit == nil {
		t.Fatal("未找到包含表格的子块")
	}
	if !strings.Contains(tableUnit.Content, "| 损失 | 任务 |") || !strings.Contains(tableUnit.Content, "| CE   | 分类 |") {
		t.Fatal("表格被从中间切开")
	}
}

func TestHeadingShortSectionsMerged(t *testing.T) {
	short := `# 文档

## 甲

短。

## 乙

也短。

## 丙

还是短。
`
	c := NewHeadingChunker()
	units := c.Chunk(short)

	// 三个 <100 token 的小节应合并，产生更少的子块。
	if len(units) >= 3 {
		t.Fatalf("超短小节未合并: 子块数 = %d", len(units))
	}
}

func TestHeadingOversizeSectionSplit(t *testing.T) {
	// 构造一个 >1000 token 的小节：约 1600 个中文字 ≈ 1067 token。
	long := strings.Repeat("这是一个用于测试超长小节二次切分的句子，内容足够长以保证超过一千个token的阈值。", 44) // ~1600+ 字
	md := "# 文档\n\n## 大节\n\n" + long + "\n"

	c := NewHeadingChunker()
	units := c.Chunk(md)

	if len(units) < 2 {
		t.Fatalf("超大小节未二次切分: 子块数 = %d", len(units))
	}
	for i, u := range units {
		if n := CountTokens(u.Content); n > OversizeTokens+WindowMaxTokens {
			t.Fatalf("unit[%d] 二次切分后仍过大: %d token", i, n)
		}
	}
	// 所有二次切分子块共享同一父块全文与标题路径。
	for i, u := range units {
		if u.TitlePath != "文档 > 大节" {
			t.Fatalf("unit[%d] 标题路径错误: %q", i, u.TitlePath)
		}
		if u.Parent == "" {
			t.Fatalf("unit[%d] 缺少父块全文", i)
		}
	}
}

func TestHeadingParentIsFullSection(t *testing.T) {
	c := NewHeadingChunker()
	units := c.Chunk(headingFixture)

	// 同一小节（如 2.3.1）的子块，其 Parent 应包含该小节全文。
	for _, u := range units {
		if strings.Contains(u.TitlePath, "2.3.1 均方误差") {
			if !strings.Contains(u.Parent, "均方误差是最常用的回归损失。") {
				t.Fatal("父块未包含完整小节内容")
			}
			return
		}
	}
	t.Fatal("未找到 2.3.1 小节")
}
