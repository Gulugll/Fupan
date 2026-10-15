import { useMemo } from "react";
import { marked } from "marked";
import DOMPurify from "dompurify";

// 安全渲染卡片内的 Markdown（LLM 生成内容可能带 **加粗**、列表、代码、表格等）。
// marked 解析 + DOMPurify 消毒，防注入。
marked.setOptions({ gfm: true, breaks: true });

// LLM 输出常在表格前漏空行，导致 marked 不识别表格。在代码围栏之外，
// 给紧跟在普通文本后的表格行补一个空行。
function normalize(text: string): string {
  const parts = text.split(/(```[^`]*```)/g); // 成对围栏整段保护
  return parts
    .map((p) => {
      if (p.startsWith("```")) return p;
      return p.replace(/(^|[^\n|])\n(\|[^\n]*\|)/g, "$1\n\n$2");
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
