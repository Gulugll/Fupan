import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, Card, ChatMessage } from "../api/client";
import MarkdownView from "../components/Markdown";

// Agent 问答复习：选一张卡 → 与 agent 对话检验掌握度 → agent 给判级建议 → 用户确认才计分。
const RATING_LABELS: Record<number, string> = {
  1: "完全想不起来",
  2: "有点模糊",
  3: "想起来了",
  4: "秒答",
};

// 会话里展示给用户的消息（过滤 system/tool 中间态）。
type Visible = { role: "user" | "assistant"; content: string };

export default function AgentReview() {
  const { libId } = useParams();
  const [cards, setCards] = useState<Card[]>([]);
  const [cardId, setCardId] = useState("");
  const [visible, setVisible] = useState<Visible[]>([]);
  const [history, setHistory] = useState<ChatMessage[]>([]); // 全量 messages（含 system/tool）
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [suggested, setSuggested] = useState(0);
  const [error, setError] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!libId) return;
    api.listCards(libId).then(setCards).catch((e) => setError(e.message));
  }, [libId]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [visible, suggested]);

  const pickCard = (id: string) => {
    setCardId(id);
    setVisible([]);
    setHistory([]);
    setSuggested(0);
    setError("");
  };

  const send = async (text?: string) => {
    if (!libId || !cardId || busy) return;
    const userMsg = text ?? input.trim();
    if (!userMsg) return;
    setInput("");
    setBusy(true);
    setError("");
    const nextVisible = [...visible, { role: "user" as const, content: userMsg }];
    setVisible(nextVisible);
    try {
      const res = await api.agentChat(libId, cardId, history);
      setHistory(res.messages);
      const tail = res.messages.slice(history.length);
      for (const m of tail) {
        if (m.role === "assistant" && m.content) {
          setVisible((v) => [...v, { role: "assistant", content: m.content }]);
        }
      }
      setSuggested(res.suggested_rating);
    } catch (e: unknown) {
      setError((e as Error).message);
      setVisible(visible); // 回滚 user 气泡
    } finally {
      setBusy(false);
    }
  };

  const confirmRating = async () => {
    if (!suggested) return;
    try {
      await api.reviewCard(cardId, suggested);
      setSuggested(0);
      setVisible((v) => [...v, { role: "assistant", content: `已按建议评分 ${suggested}（${RATING_LABELS[suggested]}）记入复习进度。` }]);
    } catch (e: unknown) {
      setError((e as Error).message);
    }
  };

  const selected = cards.find((c) => c.id === cardId);

  return (
    <div className="mx-auto flex h-[calc(100vh-4rem)] max-w-3xl flex-col px-6 py-6">
      {/* 卡片选择 */}
      <div className="mb-4 flex items-center gap-3">
        <h2 className="font-display text-xl font-bold">Agent 复习</h2>
        <select
          value={cardId}
          onChange={(e) => pickCard(e.target.value)}
          className="max-w-md flex-1 rounded-lg border border-line bg-card px-3 py-2 text-sm"
        >
          <option value="">— 选择卡片 —</option>
          {cards.map((c) => (
            <option key={c.id} value={c.id}>
              [{c.category || "未分类"}] {c.front.slice(0, 40)}
            </option>
          ))}
        </select>
        <Link to={`/library/${libId}`} className="text-xs text-faint hover:text-ink">
          ← 返回
        </Link>
      </div>

      {selected && (
        <p className="mb-3 rounded-lg border border-line bg-card px-3 py-2 text-xs text-faint">
          当前卡：<span className="text-ink">{selected.front}</span>
        </p>
      )}

      {/* 对话区 */}
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto rounded-xl border border-line bg-card p-4 paper-shadow">
        {!cardId && <p className="py-10 text-center text-xs text-faint">先选一张卡片</p>}
        {visible.map((m, i) => (
          <div key={i} className={m.role === "user" ? "text-right" : ""}>
            <div
              className={`inline-block max-w-[85%] rounded-xl px-4 py-2.5 text-left text-sm leading-6 ${
                m.role === "user"
                  ? "bg-ink text-paper"
                  : "border border-line bg-paper text-ink"
              }`}
            >
              <MarkdownView text={m.content} className={m.role === "user" ? "[&_p]:text-paper" : ""} />
            </div>
          </div>
        ))}
        {busy && <p className="text-xs text-faint">Agent 思考中……</p>}

        {/* 判级建议确认条（HITL：agent 只建议，用户裁决） */}
        {suggested > 0 && (
          <div className="rounded-lg border border-[#c07f2d]/40 bg-[#faf3ec] p-3">
            <p className="text-sm text-ink">
              Agent 建议判级：<b>{suggested}（{RATING_LABELS[suggested]}）</b>
            </p>
            <div className="mt-2 flex gap-2">
              <button
                onClick={confirmRating}
                className="rounded-lg bg-[#5a6f4e] px-4 py-1.5 text-xs text-white hover:bg-[#4c5f42]"
              >
                按建议评分并计入进度
              </button>
              <button
                onClick={() => setSuggested(0)}
                className="rounded-lg border border-line px-4 py-1.5 text-xs text-ink-soft hover:border-ink-soft"
              >
                忽略
              </button>
            </div>
          </div>
        )}
        {error && <p className="text-sm text-seal">{error}</p>}
        <div ref={bottomRef} />
      </div>

      {/* 输入区 */}
      <div className="mt-3 flex gap-2">
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && send()}
          disabled={!cardId || busy}
          placeholder={cardId ? "回答 Agent 的问题，或向它提问/质疑这张卡……" : "先选卡片"}
          className="flex-1 rounded-lg border border-line bg-card px-4 py-2.5 text-sm focus:border-ink-soft focus:outline-none disabled:opacity-50"
        />
        <button
          onClick={() => send()}
          disabled={!cardId || busy || !input.trim()}
          className="rounded-lg bg-ink px-5 py-2.5 text-sm text-paper hover:bg-ink/85 disabled:opacity-40"
        >
          发送
        </button>
      </div>
    </div>
  );
}
