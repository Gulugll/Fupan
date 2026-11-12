import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, Library, ReviewStats } from "../api/client";

interface DeckData {
  lib: Library;
  stats: ReviewStats | null;
}

// 首页总览：四统计卡 + 库卡片网格（v1 纸感书房 / v3 深色双主题共用骨架）。
export default function LibraryHome() {
  const [decks, setDecks] = useState<DeckData[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    api.listLibraries().then(async (ls) => {
      const decks = await Promise.all(
        ls.map(async (lib) => ({
          lib,
          stats: await api.reviewStats(lib.id).catch(() => null),
        }))
      );
      setDecks(decks);
      setLoaded(true);
    }).catch(() => setLoaded(true));
  }, []);

  // ---- 聚合统计 ----
  const dueToday = decks.reduce((n, d) => n + (d.stats?.due_now ?? 0), 0);
  const totalCards = decks.reduce((n, d) => n + (d.stats?.total_cards ?? 0), 0);
  const allHistory = decks.flatMap((d) => d.stats?.history ?? []);
  const goodSum = allHistory.reduce((n, h) => n + h.good, 0);
  const reviewSum = allHistory.reduce((n, h) => n + h.reviews, 0);
  const retention = reviewSum > 0 ? Math.round((goodSum / reviewSum) * 100) : null;
  // 连续复习天数：从今天（或昨天）向前数 history 有复习记录的天数
  const streak = (() => {
    const byDate = new Map<string, number>();
    for (const h of allHistory) byDate.set(h.date, (byDate.get(h.date) ?? 0) + h.reviews);
    let n = 0;
    const day = new Date();
    if (!byDate.get(day.toISOString().slice(0, 10))) day.setDate(day.getDate() - 1); // 今天还没复习不打断
    while ((byDate.get(day.toISOString().slice(0, 10)) ?? 0) > 0) {
      n++;
      day.setDate(day.getDate() - 1);
    }
    return n;
  })();

  const steps = [
    ["建库", "在左侧新建一个知识库"],
    ["导入", "上传 Markdown / TXT 笔记，自动按标题切片"],
    ["制卡", "勾选小节生成复习卡片"],
    ["复习", "到期即复习，每张卡都能溯源回原文"],
  ];

  return (
    <div className="mx-auto max-w-5xl px-8 py-10">
      <h2 className="font-display text-3xl font-bold tracking-wide">知 识 库</h2>
      <p className="mt-2 text-xs tracking-widest text-faint">忘记的，推上山；记住的，铸成磐石。</p>

      {loaded && decks.length > 0 && (
        <>
          <div className="mt-8 grid grid-cols-2 gap-4 md:grid-cols-4">
            <div className="rounded-xl border border-line bg-card px-5 py-4 paper-shadow">
              <div className="font-display text-3xl font-bold text-accent">{dueToday}</div>
              <div className="mt-1 text-xs text-ink-soft">今日待复习</div>
            </div>
            <div className="rounded-xl border border-line bg-card px-5 py-4 paper-shadow">
              <div className="font-display text-3xl font-bold text-accent">{totalCards}</div>
              <div className="mt-1 text-xs text-ink-soft">卡片总数</div>
            </div>
            <div className="rounded-xl border border-line bg-card px-5 py-4 paper-shadow">
              <div className="font-display text-3xl font-bold text-accent">{retention === null ? "—" : `${retention}%`}</div>
              <div className="mt-1 text-xs text-ink-soft">近 30 天答对率</div>
            </div>
            <div className="rounded-xl border border-line bg-card px-5 py-4 paper-shadow">
              <div className="font-display text-3xl font-bold text-accent">{streak} 天</div>
              <div className="mt-1 text-xs text-ink-soft">连续复习</div>
            </div>
          </div>

          <div className="mt-10 mb-4 flex items-center gap-3">
            <h3 className="font-display text-base font-bold">我的库</h3>
            <span className="h-px flex-1 bg-line" />
          </div>
          <div className="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
            {decks.map(({ lib, stats }) => {
              const total = stats?.total_cards ?? 0;
              const mastered = stats?.state_counts?.["review"] ?? 0;
              const pct = total > 0 ? Math.round((mastered / total) * 100) : 0;
              const due = stats?.due_now ?? 0;
              return (
                <Link key={lib.id} to={`/library/${lib.id}`} className="deck-stack group block">
                  <div className="rounded-xl border border-line bg-card p-5 transition-colors group-hover:border-accent">
                    <h4 className="font-display text-lg font-bold">{lib.name}</h4>
                    <p className="mt-1 text-xs text-faint">{total} 张卡片</p>
                    <div className="deck-progress mt-4 h-1.5 overflow-hidden rounded-full bg-line">
                      <i className="block h-full rounded-full bg-accent" style={{ width: `${pct}%` }} />
                    </div>
                    <div className="mt-2 flex items-center justify-between text-xs">
                      <span className="text-ink-soft">已进入复习轨 {pct}%</span>
                      {due > 0 ? (
                        <span className="font-medium text-seal">{due} 张到期</span>
                      ) : (
                        <span className="text-faint">全部完成 ✓</span>
                      )}
                    </div>
                  </div>
                </Link>
              );
            })}
          </div>
        </>
      )}

      {loaded && decks.length === 0 && (
        <div className="mx-auto mt-14 max-w-xl text-center">
          <div className="mx-auto mb-8 flex h-16 w-16 items-center justify-center rounded-md bg-seal font-display text-4xl text-[#f8f3e7] shadow-[inset_0_0_0_2px_rgba(248,243,231,0.5)]">
            磐
          </div>
          <h2 className="font-display text-3xl font-bold">欢迎来到复磐</h2>
          <p className="mt-3 text-sm leading-6 text-ink-soft">忘记的，推上山；记住的，铸成磐石。</p>
          <ol className="mt-10 space-y-3 text-left">
            {steps.map(([t, d], i) => (
              <li key={t} className="flex items-baseline gap-3 rounded-lg border border-line bg-card px-4 py-3 paper-shadow">
                <span className="font-display text-seal">{["一", "二", "三", "四"][i]}</span>
                <span className="text-sm font-medium">{t}</span>
                <span className="text-sm text-ink-soft">{d}</span>
              </li>
            ))}
          </ol>
          <p className="mt-8 text-xs text-faint">从左侧「＋ 新建知识库」开始</p>
          <Link to="/settings" className="mt-6 inline-block text-xs text-faint underline hover:text-ink-soft">
            首次使用？先到设置页配置模型服务
          </Link>
        </div>
      )}
    </div>
  );
}
