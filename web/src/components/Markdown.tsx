import { useMemo } from "react";
import { marked } from "marked";
import DOMPurify from "dompurify";

// 安全渲染卡片内的 Markdown（LLM 生成内容可能带 **加粗**、列表、代码、表格等）。
// marked 解析 + DOMPurify 消毒，防注入。
marked.setOptions({ gfm: true, breaks: true });

// LLM 输出的 Markdown 常见问题归一化（仅处理代码围栏之外的部分）：
// 1. 表格前漏空行（marked 不识别）→ 补空行；
// 2. 全角管道「｜」→ 半角「|」；
// 3. 表格缺第二行分隔行 → 在表头后补 |---| 。
function normalize(text: string): string {
  const parts = text.split(/(```[^]*?```)/g); // 成对围栏整段保护
  return parts
    .map((p) => {
      if (p.startsWith("```")) return p;
      let s = p.replace(/｜/g, "|");
      s = s.replace(/(^|[^\n|])\n(\|[^\n]*\|)/g, "$1\n\n$2");
      // 逐行扫描：表格块第一行（表头）后若直接跟数据行（缺分隔行）则补一行。
      const lines = s.split("\n");
      const out: string[] = [];
      for (let i = 0; i < lines.length; i++) {
        out.push(lines[i]);
        const isRow = (l: string) => /^\s*\|.*\|\s*$/.test(l);
        const isSep = (l: string) => /^\s*\|[\s:|-]+\|\s*$/.test(l);
        const prevIsRow = i > 0 && isRow(lines[i - 1]);
        if (
          isRow(lines[i]) && !prevIsRow && // 只在表格块首行（表头）后检查
          i + 1 < lines.length && isRow(lines[i + 1]) && !isSep(lines[i + 1])
        ) {
          const cols = (lines[i].match(/\|/g) || []).length - 1;
          out.push("|" + Array(Math.max(cols, 1)).fill(" --- ").join("|") + "|");
        }
      }
      return out.join("\n");
    })
    .join("");
}

export default function Markdown({
  text,
  className = "",
  inline = false,
}: {
  text: string;
  className?: string;
  inline?: boolean; // true 时渲染 span（用于 button 等不能嵌 div 的容器内）
}) {
  const html = useMemo(
    () => DOMPurify.sanitize(marked.parse(normalize(text ?? ""), { async: false }) as string),
    [text]
  );
  return inline ? (
    <span className={`md-body ${className}`} dangerouslySetInnerHTML={{ __html: html }} />
  ) : (
    <div className={`md-body ${className}`} dangerouslySetInnerHTML={{ __html: html }} />
  );
}
