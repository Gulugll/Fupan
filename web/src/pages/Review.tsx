import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, QueueItem, SourceInfo } from "../api/client";
import Markdown from "../components/Markdown";

// 复习页：实体翻卡（3D）→ 1–4 自评 → 下一张；空格翻卡、1–4 评分。
// 等级与 FSRS rating 一一对应（设计文档 3 模式 A）。
const RATINGS = [
  { level: 1, label: "完全想不起来", key: "1", cls: "bg-seal hover:bg-seal-deep border-seal" },
  { level: 2, label: "有点模糊", key: "2", cls: "bg-[#c07f2d] hover:bg-[#a96e26] border-[#c07f2d]" },
  { level: 3, label: "想起来了", key: "3", cls: "bg-ink hover:bg-ink/85 border-ink" },
  { level: 4, label: "秒答", key: "4", cls: "bg-[#5a6f4e] hover:bg-[#4c5f42] border-[#5a6f4e]" },
] as const;

export default function Review() {
  const { libId } = useParams();
  const [queue, setQueue] = useState<QueueItem[]>([]);
  const [idx, setIdx] = useState(0);
  const [flipped, setFlipped] = useState(false);
  const [source, setSource] = useState<SourceInfo | null>(null);
  const [showSource, setShowSource] = useState(false);
  const [loading, setLoading] = useState(true);
  const [done, setDone] = useState(false);

  useEffect(() => {
    if (!libId) return;
    api.reviewQueue(libId).then((res) => {
      setQueue(res.items);
      setLoading(false);
      if (res.items.length === 0) setDone(true);
    });
  }, [libId]);

  const card = queue[idx];

  const flip = useCallback(async () => {
    setFlipped(true);
    if (card) {
      try {
        setSource(await api.cardSource(card.id));
      } catch {
        setSource(null);
      }
    }
  }, [card]);

  const rate = useCallback(async (rating: number) => {
    if (!card) return;
    await api.reviewCard(card.id, rating);
    setFlipped(false);
    setShowSource(false);
    setSource(null);
    if (idx + 1 >= queue.length) {
      setDone(true);
    } else {
      setIdx(idx + 1);
    }
  }, [card, idx, queue.length]);

  // 键盘：空格翻卡，1–4 评分，Esc 关溯源弹层。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (showSource) {
        if (e.key === "Escape") setShowSource(false);
        return;
      }
      if (e.code === "Space" || e.key === "Enter") {
        e.preventDefault();
        if (!flipped) flip();
        return;
      }
      if (flipped && ["1", "2", "3", "4"].includes(e.key)) {
        rate(Number(e.key));
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [flipped, showSource, flip, rate]);

  if (loading) {
    return <div className="p-16 text-center text-sm text-faint">正在取队列……</div>;
  }

  if (done) {
    return (
      <div className="mx-auto max-w-lg px-6 pt-28 text-center">
        <div className="mx-auto mb-6 flex h-14 w-14 items-center justify-center rounded-md bg-seal font-display text-3xl text-[#f8f3e7]">
          磐
        </div>
        <h2 className="font-display text-2xl font-bold">今日复习完成</h2>
        <p className="mt-2 text-sm text-ink-soft">滚动的石头不长苔藓，明天记得再来。</p>
        <Link
          to={`/library/${libId}`}
          className="mt-8 inline-block rounded-md border border-line bg-card px-5 py-2 text-sm text-ink transition-colors hover:border-ink-soft paper-shadow"
        >
          返回知识库
        </Link>
      </div>
    );
  }

  const progress = ((idx + (flipped ? 0.5 : 0)) / queue.length) * 100;

  return (
    <div className="mx-auto max-w-xl px-6 pt-10">
      {/* 进度：斜坡上的石头 */}
      <div className="mb-5">
        <div className="mb-1.5 flex items-center justify-between text-xs text-faint">
          <span>
            {card?.is_new ? "新卡" : "复习"} · 第 {idx + 1} 张 / 共 {queue.length} 张
          </span>
          <span>空格翻卡，1–4 评分</span>
        </div>
        <div className="relative h-1 rounded-full bg-line">
          <div
            className="absolute inset-y-0 left-0 rounded-full bg-seal/70 transition-all duration-500"
            style={{ width: `${progress}%` }}
          />
          <span
            className="absolute top-1/2 h-2.5 w-2.5 -translate-y-1/2 rounded-full bg-ink transition-all duration-500"
            style={{ left: `calc(${progress}% - 5px)` }}
          />
        </div>
      </div>

      {/* 卡堆 + 实体翻卡（grid 叠放，高度随内容自适应） */}
      <div className="flip-scene relative">
        <div className="card-next" aria-hidden />
        <div className={`flip-inner ${flipped ? "is-flipped" : ""}`}>
          {/* 正面：问题 */}
          <button
            onClick={() => !flipped && flip()}
            className="flip-face flex w-full cursor-pointer flex-col rounded-xl border border-line bg-card p-8 text-left paper-shadow focus-visible:outline-2 focus-visible:outline-seal"
          >
            <span className="text-[11px] tracking-widest text-faint">问</span>
            <span className="flex flex-1 items-center justify-center py-4">
              <Markdown text={card?.front ?? ""} inline className="font-display text-xl font-medium leading-relaxed" />
            </span>
            <span className="text-center text-[11px] text-faint">点击卡片或按空格翻面</span>
          </button>

          {/* 背面：答案 */}
          <div className="flip-face flip-back flex w-full flex-col rounded-xl border border-line bg-card p-8 paper-shadow">
            <span className="text-[11px] tracking-widest text-faint">答</span>
            <span className="block pt-3 text-sm leading-6 text-ink-soft">
              <Markdown text={card?.front ?? ""} inline className="text-sm" />
            </span>
            <span className="my-3 block border-t border-dashed border-line" />
            <span className="block flex-1">
              <Markdown text={card?.back ?? ""} className="text-lg leading-relaxed" />
            </span>
          </div>
        </div>
      </div>

      {/* 溯源徽章 */}
      {flipped && source && (
        <>
          <button
            onClick={() => setShowSource(true)}
            className="mt-3 inline-flex items-center gap-1 rounded-full border border-line bg-card px-3 py-1 text-xs text-ink-soft transition-colors hover:border-ink-soft hover:text-ink"
          >
            <span className="text-seal">溯源</span>
            {source.doc_title}
            {source.title_path && <span className="text-faint"> › {source.title_path}</span>}
          </button>
          {showSource && (
            <div
              className="fixed inset-0 z-50 flex items-center justify-center bg-ink/45 p-4"
              onClick={() => setShowSource(false)}
            >
              <div
                className="max-h-[70vh] w-full max-w-xl overflow-y-auto rounded-xl border border-line bg-card p-6 paper-shadow"
                onClick={(e) => e.stopPropagation()}
              >
                <div className="mb-3 flex items-center justify-between">
                  <p className="font-medium">{source.doc_title}</p>
                  <button onClick={() => setShowSource(false)} className="text-faint hover:text-ink">
                    ✕
                  </button>
                </div>
                {source.title_path && (
                  <p className="mb-3 text-xs text-faint">{source.title_path}</p>
                )}
                <SourceHighlight info={source} />
              </div>
            </div>
          )}
        </>
      )}

      {/* 自评：翻面前是翻卡按钮，翻面后是四档评分（带快捷键提示） */}
      <div className="mt-6">
        {!flipped ? (
          <button
            onClick={flip}
            className="w-full rounded-xl bg-ink py-3 text-sm text-paper transition-colors hover:bg-ink/85"
          >
            翻面 · 先在心里回忆
          </button>
        ) : (
          <div className="grid grid-cols-4 gap-2.5">
            {RATINGS.map((r) => (
              <button
                key={r.level}
                onClick={() => rate(r.level)}
                title={r.label}
                className={`rounded-xl border py-3 text-xs text-paper transition-transform active:scale-95 ${r.cls}`}
              >
                <span className="block font-display text-lg font-bold">{r.key}</span>
                {r.label}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// 原文展示：父块全文，其中子块文本以荧光笔黄高亮（溯源回跳的核心视觉）。
// 展示时去掉原始 Markdown 标题行，只留正文。
function SourceHighlight({ info }: { info: SourceInfo }) {
  const strip = (s: string) => s.replace(/^#{1,6}\s+[^\n]*\n+/gm, "");
  const parent = strip(info.parent_text || info.child_text);
  const child = strip(info.child_text);
  const pos = parent.indexOf(child);
  return (
    <div className="whitespace-pre-wrap rounded-lg bg-paper p-4 font-display text-sm leading-8 text-ink">
      {pos >= 0 ? (
        <>
          {parent.slice(0, pos)}
          <mark className="rounded bg-mark px-0.5">{child}</mark>
          {parent.slice(pos + child.length)}
        </>
      ) : (
        <>
          {parent}
          <hr className="my-2 border-line" />
          <mark className="rounded bg-mark px-0.5">{child}</mark>
        </>
      )}
    </div>
  );
}
