import { useEffect, useState } from "react";
import { api, ReviewStats } from "../api/client";

// 记忆统计（基础版）：今日进度 + 未来 7 天到期分布 + 卡片状态分布。
// 仅提供数据可视化骨架，视觉设计后续迭代。
export default function MemoryStats({ libId }: { libId: string }) {
  const [st, setSt] = useState<ReviewStats | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api.reviewStats(libId).then(setSt).catch((e) => setError(e.message));
  }, [libId]);

  if (error) return <p className="text-xs text-seal">统计加载失败：{error}</p>;
  if (!st) return <p className="text-xs text-faint">正在加载统计……</p>;

  const maxCount = Math.max(1, ...st.forecast.map((d) => d.count));
  const today = new Date();
  const weekdays = ["日", "一", "二", "三", "四", "五", "六"];

  const stateLabels: [string, string, string][] = [
    ["new", "新卡", "#a8763e"],
    ["learning", "学习中", "#c07f2d"],
    ["review", "复习中", "#5a6f4e"],
    ["relearning", "重学中", "#8a5a44"],
  ];

  return (
    <div className="rounded-xl border border-line bg-card p-5 paper-shadow">
      {/* 今日 / 明日进度数字 */}
      <div className="grid grid-cols-5 gap-3 text-center">
        {[
          ["今日已复习", st.today_reviews, "次"],
          ["今日新卡", st.today_new, "张"],
          ["今天待复习", st.forecast[0]?.count ?? 0, "张"],
          ["明天预计", st.forecast[1]?.count ?? 0, "张"],
          ["卡片总数", st.total_cards, "张"],
        ].map(([label, value, unit]) => (
          <div key={label as string}>
            <p className="font-display text-2xl font-bold text-ink">
              {value as number}
              <span className="ml-0.5 text-xs font-normal text-faint">{unit as string}</span>
            </p>
            <p className="mt-0.5 text-[11px] text-faint">{label as string}</p>
          </div>
        ))}
      </div>

      {/* 未来 7 天到期分布（记忆曲线骨架） */}
      <div className="mt-5 border-t border-dashed border-line pt-4">
        <p className="mb-2 text-[11px] tracking-widest text-faint">未来 7 天到期（今天含逾期）</p>
        <div className="flex h-20 items-end gap-2">
          {st.forecast.map((d, i) => (
            <div key={d.date} className="flex flex-1 flex-col items-center gap-1">
              <span className="text-[10px] text-ink-soft">{d.count > 0 ? d.count : ""}</span>
              <div
                className={`w-full rounded-t ${i === 0 ? "bg-seal/80" : "bg-[#5a6f4e]/60"}`}
                style={{ height: `${Math.max(4, (d.count / maxCount) * 56)}px` }}
                title={`${d.date}：${d.count} 张到期`}
              />
              <span className="text-[10px] text-faint">
                {i === 0 ? "今天" : i === 1 ? "明天" : weekdays[(today.getDay() + i) % 7]}
              </span>
            </div>
          ))}
        </div>
      </div>

      {/* 卡片状态分布 */}
      <div className="mt-4 flex flex-wrap gap-2 border-t border-dashed border-line pt-4">
        {stateLabels.map(([key, label, color]) => {
          const n = st.state_counts[key] ?? 0;
          if (n === 0) return null;
          return (
            <span
              key={key}
              className="rounded-full px-2.5 py-0.5 text-[11px]"
              style={{ backgroundColor: `${color}1a`, color }}
            >
              {label} {n}
            </span>
          );
        })}
      </div>
    </div>
  );
}
