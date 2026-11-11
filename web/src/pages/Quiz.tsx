import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, Card } from "../api/client";
import Markdown from "../components/Markdown";

// 随机题目模式：选分类与张数 → 随机抽卡翻看。纯浏览，不写调度与复习日志。
const COUNTS = [10, 20, 50] as const;

export default function Quiz() {
  const { libId } = useParams();
  const [categories, setCategories] = useState<string[]>([]);
  const [category, setCategory] = useState("");
  const [limit, setLimit] = useState<number>(10);
  const [cards, setCards] = useState<Card[] | null>(null); // null = 还在设置页
  const [idx, setIdx] = useState(0);
  const [showBack, setShowBack] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  // 分类列表从卡片管理接口归纳（本地量级无忧）。
  useEffect(() => {
    if (!libId) return;
    api.listCards(libId).then((cs) => {
      setCategories([...new Set(cs.map((c) => c.category).filter(Boolean))]);
    }).catch(() => {});
  }, [libId]);

  const start = async () => {
    if (!libId) return;
    setLoading(true);
    setError("");
    try {
      const res = await api.quiz(libId, category, limit);
      if (res.length === 0) {
        setError("没有可抽的卡片（该分类下为空或库里还没有卡）");
        return;
      }
      setCards(res);
      setIdx(0);
      setShowBack(false);
    } catch (e: unknown) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  const next = () => {
    setShowBack(false);
    setIdx((i) => i + 1);
  };

  const progress = useMemo(
    () => (cards && cards.length > 0 ? Math.round((idx / cards.length) * 100) : 0),
    [cards, idx]
  );

  // 设置视图
  if (!cards) {
    return (
      <div className="mx-auto max-w-lg px-6 pt-16">
        <h2 className="font-display text-2xl font-bold">随机抽题</h2>
        <p className="mt-2 text-sm text-ink-soft">
          不看记忆曲线、不计分——想纯浏览过一遍时用这个模式。
        </p>

        <div className="mt-8 rounded-xl border border-line bg-card p-5 paper-shadow">
          <p className="mb-2 text-[11px] tracking-widest text-faint">分类</p>
          <div className="flex flex-wrap gap-2">
            {[{ label: "全部分类", value: "" }, ...categories.map((c) => ({ label: c, value: c }))].map(
              ({ label, value }) => (
                <button
                  key={value}
                  onClick={() => setCategory(value)}
                  className={`rounded-full border px-3 py-1 text-xs transition-colors ${
                    category === value
                      ? "border-seal bg-seal text-paper"
                      : "border-line bg-card text-ink-soft hover:border-ink-soft"
                  }`}
                >
                  {label}
                </button>
              )
            )}
          </div>

          <p className="mb-2 mt-5 text-[11px] tracking-widest text-faint">张数</p>
          <div className="flex gap-2">
            {COUNTS.map((n) => (
              <button
                key={n}
                onClick={() => setLimit(n)}
                className={`rounded-lg border px-4 py-1.5 text-xs transition-colors ${
                  limit === n
                    ? "border-seal bg-seal text-paper"
                    : "border-line bg-card text-ink-soft hover:border-ink-soft"
                }`}
              >
                {n} 张
              </button>
            ))}
          </div>

          {error && <p className="mt-4 text-sm text-seal">{error}</p>}

          <button
            onClick={start}
            disabled={loading}
            className="mt-6 w-full rounded-xl bg-ink py-3 text-sm text-paper transition-colors hover:bg-ink/85 disabled:opacity-40"
          >
            {loading ? "抽卡中……" : "开始"}
          </button>
        </div>

        <Link
          to={`/library/${libId}`}
          className="mt-5 inline-block text-xs text-faint hover:text-ink"
        >
          ← 返回知识库
        </Link>
      </div>
    );
  }

  // 一轮完成
  if (idx >= cards.length) {
    return (
      <div className="mx-auto max-w-lg px-6 pt-24 text-center">
        <div className="mx-auto mb-6 flex h-14 w-14 items-center justify-center rounded-md bg-[#5a6f4e] font-display text-3xl text-[#f8f3e7]">
          磐
        </div>
        <h2 className="font-display text-2xl font-bold">这轮过完了</h2>
        <p className="mt-2 text-sm text-ink-soft">随机过了 {cards.length} 张，本次不计入复习进度。</p>
        <div className="mt-8 flex items-center justify-center gap-3">
          <button
            onClick={start}
            className="rounded-md bg-ink px-5 py-2 text-sm text-paper transition-colors hover:bg-ink/85"
          >
            再抽一轮
          </button>
          <button
            onClick={() => setCards(null)}
            className="rounded-md border border-line bg-card px-5 py-2 text-sm text-ink transition-colors hover:border-ink-soft paper-shadow"
          >
            换分类 / 张数
          </button>
        </div>
      </div>
    );
  }

  const card = cards[idx];

  return (
    <div className="mx-auto max-w-xl px-6 pt-10">
      {/* 进度 */}
      <div className="mb-5">
        <div className="mb-1.5 flex items-center justify-between text-xs text-faint">
          <span>
            <span className="mr-2 rounded-md bg-[#5a6f4e]/10 px-1.5 py-0.5 text-[#5a6f4e]">随机抽题</span>
            {category || "全部分类"} · 第 {idx + 1} 张 / 共 {cards.length} 张
          </span>
          <button
            onClick={() => setCards(null)}
            className="rounded-md border border-line px-2 py-0.5 text-[11px] text-ink-soft transition-colors hover:border-ink-soft hover:text-ink"
          >
            退出
          </button>
        </div>
        <div className="h-1 rounded-full bg-line">
          <div
            className="h-1 rounded-full bg-[#5a6f4e]/70 transition-all duration-500"
            style={{ width: `${progress}%` }}
          />
        </div>
      </div>

      {/* 卡面：随机模式用简单正/背切换，不做 3D 翻卡 */}
      <div className="rounded-xl border border-line bg-card p-8 paper-shadow">
        <span className="text-[11px] tracking-widest text-faint">问</span>
        <div className="py-2">
          <Markdown text={card.front} inline className="font-display text-xl font-medium leading-relaxed" />
        </div>
        {showBack ? (
          <>
            <div className="my-3 border-t border-dashed border-line" />
            <span className="text-[11px] tracking-widest text-faint">答</span>
            <Markdown text={card.back} className="mt-2 text-base leading-relaxed" />
            {card.category && (
              <span className="mt-4 inline-block rounded-md bg-[#5a6f4e]/10 px-2 py-0.5 text-xs text-[#5a6f4e]">
                {card.category}
              </span>
            )}
          </>
        ) : null}
      </div>

      <div className="mt-6">
        {!showBack ? (
          <button
            onClick={() => setShowBack(true)}
            className="w-full rounded-xl bg-ink py-3 text-sm text-paper transition-colors hover:bg-ink/85"
          >
            显示答案
          </button>
        ) : (
          <button
            onClick={next}
            className="w-full rounded-xl bg-[#5a6f4e] py-3 text-sm text-white transition-colors hover:bg-[#4c5f42]"
          >
            {idx + 1 >= cards.length ? "完成本轮" : "下一张"}
          </button>
        )}
      </div>
    </div>
  );
}
