import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api, QueueItem, SourceInfo } from "../api/client";

// 复习页：翻卡 → 1–4 自评 → 下一张；溯源徽章点击弹出原文高亮。
// 等级与 FSRS rating 一一对应（设计文档 3 模式 A）。
const RATINGS = [
  { level: 1, label: "完全想不起来", short: "再来看看", cls: "bg-red-600 hover:bg-red-500" },
  { level: 2, label: "模糊记得 / 想了很久", short: "有点模糊", cls: "bg-orange-500 hover:bg-orange-400" },
  { level: 3, label: "想起来了", short: "想起来了", cls: "bg-stone-700 hover:bg-stone-600" },
  { level: 4, label: "秒答 / 非常牢固", short: "秒答", cls: "bg-green-700 hover:bg-green-600" },
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

  const flip = async () => {
    setFlipped(true);
    if (card) {
      try {
        setSource(await api.cardSource(card.id));
      } catch {
        setSource(null);
      }
    }
  };

  const rate = async (rating: number) => {
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
  };

  if (loading) {
    return <div className="p-16 text-center text-sm text-stone-400">正在取队列……</div>;
  }

  if (done) {
    return (
      <div className="p-16 max-w-lg mx-auto text-center pt-28">
        <div className="text-5xl mb-4">🏔️</div>
        <h2 className="text-xl font-bold mb-2">今日复习完成</h2>
        <p className="text-sm text-stone-500">滚动的石头不长苔藓。明天记得再来。</p>
        <a
          href={`#/library/${libId}`}
          className="mt-6 inline-block rounded-md border border-stone-300 px-4 py-2 text-sm hover:bg-stone-50"
        >
          ← 返回知识库
        </a>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-xl px-6 pt-12">
      {/* 进度 */}
      <div className="mb-6 flex items-center justify-between text-xs text-stone-400">
        <span>{card?.is_new ? "🌱 新卡" : "🔁 复习卡"} · 状态 {card?.state}</span>
        <span>{idx + 1} / {queue.length}</span>
      </div>

      {/* 卡面 */}
      <div
        className="min-h-64 rounded-2xl border border-stone-200 bg-white p-8 shadow-sm flex flex-col"
        onClick={() => !flipped && flip()}
      >
        {!flipped ? (
          <div className="flex flex-1 items-center justify-center text-center">
            <p className="text-lg font-medium">{card?.front}</p>
          </div>
        ) : (
          <div className="flex flex-1 flex-col">
            <p className="text-sm text-stone-400 mb-2">{card?.front}</p>
            <p className="text-lg border-t border-stone-100 pt-4">{card?.back}</p>
          </div>
        )}
      </div>

      {/* 溯源徽章 */}
      {flipped && source && (
        <>
          <button
            onClick={() => setShowSource(true)}
            className="mt-3 inline-flex items-center gap-1 rounded-full bg-stone-100 px-3 py-1 text-xs text-stone-500 hover:bg-stone-200"
          >
            📄 {source.doc_title}
            {source.title_path && <> › {source.title_path}</>}
          </button>
          {showSource && (
            <div
              className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
              onClick={() => setShowSource(false)}
            >
              <div
                className="max-w-xl w-full mx-4 max-h-[70vh] overflow-y-auto rounded-xl bg-white p-6"
                onClick={(e) => e.stopPropagation()}
              >
                <div className="mb-3 flex items-center justify-between">
                  <p className="text-sm font-medium">📄 {source.doc_title}</p>
                  <button onClick={() => setShowSource(false)} className="text-stone-400 hover:text-stone-600">✕</button>
                </div>
                {source.title_path && (
                  <p className="mb-3 text-xs text-stone-400">路径：{source.title_path}</p>
                )}
                {/* 父块全文，子块部分高亮 */}
                <SourceHighlight info={source} />
              </div>
            </div>
          )}
        </>
      )}

      {/* 自评按钮：翻面前隐藏 */}
      <div className="mt-6">
        {!flipped ? (
          <button
            onClick={flip}
            className="w-full rounded-xl bg-stone-800 py-3 text-sm text-white hover:bg-stone-700"
          >
            翻面（先在心里回忆）
          </button>
        ) : (
          <div className="grid grid-cols-4 gap-2">
            {RATINGS.map((r) => (
              <button
                key={r.level}
                onClick={() => rate(r.level)}
                title={r.label}
                className={`rounded-xl py-3 text-xs text-white ${r.cls}`}
              >
                <span className="block text-base font-bold">{r.level}</span>
                {r.short}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// 原文展示：父块全文，其中子块文本高亮（溯源回跳的核心视觉）。
function SourceHighlight({ info }: { info: SourceInfo }) {
  const parent = info.parent_text || info.child_text;
  const child = info.child_text;
  const pos = parent.indexOf(child);
  return (
    <div className="whitespace-pre-wrap rounded-lg bg-stone-50 p-4 text-sm leading-7 text-stone-700">
      {pos >= 0 ? (
        <>
          {parent.slice(0, pos)}
          <mark className="rounded bg-yellow-200 px-0.5">{child}</mark>
          {parent.slice(pos + child.length)}
        </>
      ) : (
        <>
          {parent}
          <hr className="my-2 border-stone-200" />
          <mark className="rounded bg-yellow-200 px-0.5">{child}</mark>
        </>
      )}
    </div>
  );
}
