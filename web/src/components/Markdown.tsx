import { useMemo } from "react";
import { marked } from "marked";
import DOMPurify from "dompurify";

// 安全渲染卡片内的 Markdown（LLM 生成内容可能带 **加粗**、列表、代码等）。
// marked 解析 + DOMPurify 消毒，防注入。
marked.setOptions({ gfm: true, breaks: true });

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
    () => DOMPurify.sanitize(marked.parse(text ?? "", { async: false }) as string),
    [text]
  );
  return inline ? (
    <span className={`md-body ${className}`} dangerouslySetInnerHTML={{ __html: html }} />
  ) : (
    <div className={`md-body ${className}`} dangerouslySetInnerHTML={{ __html: html }} />
  );
}
