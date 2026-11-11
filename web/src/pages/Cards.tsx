import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { api, Card, CardProposal } from "../api/client";

// 卡片管理：库内卡片列表 + 分类筛选 + 编辑（正/背面/分类）+ 删除（软删，复习队列不再出现）
// + Agent 写回提案区（HITL：采纳/拒绝）。
export default function Cards() {
  const { libId } = useParams();
  const [cards, setCards] = useState<Card[]>([]);
  const [activeCat, setActiveCat] = useState<string>("");
  const [editingId, setEditingId] = useState<string>("");
  const [draft, setDraft] = useState({ front: "", back: "", category: "" });
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [proposals, setProposals] = useState<CardProposal[]>([]);

  const load = () =>
    libId && api.listCards(libId).then(setCards).catch((e) => setError(e.message));
  const loadProposals = () =>
    libId && api.listProposals(libId).then(setProposals).catch(() => {});
  useEffect(() => {
    load();
    loadProposals();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [libId]);

  const decide = async (id: string, apply: boolean) => {
    try {
      if (apply) await api.applyProposal(id);
      else await api.rejectProposal(id);
      loadProposals();
      load();
    } catch (e: unknown) {
      setError((e as Error).message);
    }
  };

  // 分类列表由全量卡片归纳，前端过滤即可（本地单用户，量级无忧）。
  const categories = useMemo(
    () => [...new Set(cards.map((c) => c.category).filter(Boolean))],
    [cards]
  );
  const shown = activeCat ? cards.filter((c) => c.category === activeCat) : cards;

  const startEdit = (c: Card) => {
    setEditingId(c.id);
    setDraft({ front: c.front, back: c.back, category: c.category });
    setError("");
  };

  const save = async () => {
    if (!draft.front.trim() || !draft.back.trim()) {
      setError("正面和背面不能为空");
      return;
    }
    setSaving(true);
    try {
      await api.updateCard(editingId, draft);
      setEditingId("");
      await load();
    } catch (e: unknown) {
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const remove = async (c: Card) => {
    if (!confirm(`删除卡片「${c.front.slice(0, 30)}」？复习进度将不再显示，原文不受影响。`)) return;
    await api.deleteCard(c.id).catch((e) => setError(e.message));
    load();
  };

  return (
    <div className="mx-auto max-w-3xl px-8 py-8">
      <div className="mb-6 flex items-center justify-between">
        <h2 className="font-display text-2xl font-bold">
          卡片管理
          <span className="ml-3 text-sm font-normal text-faint">{shown.length} 张</span>
        </h2>
      </div>

      {/* 分类筛选 */}
      {categories.length > 0 && (
        <div className="mb-5 flex flex-wrap gap-2">
          {[{ label: "全部", value: "" }, ...categories.map((c) => ({ label: c, value: c }))].map(
            ({ label, value }) => (
              <button
                key={value}
                onClick={() => setActiveCat(value)}
                className={`rounded-full border px-3 py-1 text-xs transition-colors ${
                  activeCat === value
                    ? "border-seal bg-seal text-paper"
                    : "border-line bg-card text-ink-soft hover:border-ink-soft"
                }`}
              >
                {label}
              </button>
            )
          )}
        </div>
      )}

      {error && <p className="mb-3 text-sm text-seal">{error}</p>}

      {/* Agent 写回提案区（HITL） */}
      {proposals.length > 0 && (
        <div className="mb-6 rounded-xl border border-[#c07f2d]/40 bg-[#faf3ec] p-4">
          <p className="mb-2 text-[11px] tracking-widest text-[#a8763e]">
            AGENT 提案 · {proposals.length} 条待裁决
          </p>
          <ul className="space-y-2">
            {proposals.map((p) => {
              const payload = JSON.parse(p.payload) as {
                front?: string;
                back?: string;
                category?: string;
              };
              return (
                <li key={p.id} className="rounded-lg border border-line bg-card p-3 text-sm">
                  <div className="flex items-center gap-2">
                    <span className="rounded-md bg-[#a8763e]/10 px-1.5 py-0.5 text-xs text-[#a8763e]">
                      {p.kind === "edit" ? "修改卡片" : "新增卡片"}
                    </span>
                    {p.kind === "new" && payload.category && (
                      <span className="text-xs text-faint">{payload.category}</span>
                    )}
                  </div>
                  <p className="mt-1.5 font-medium">{payload.front}</p>
                  <p className="mt-0.5 text-xs text-faint">{payload.back}</p>
                  {p.rationale && (
                    <p className="mt-1.5 text-xs text-ink-soft">
                      <span className="text-[#a8763e]">理由</span> {p.rationale}
                    </p>
                  )}
                  <div className="mt-2 flex gap-2">
                    <button
                      onClick={() => decide(p.id, true)}
                      className="rounded-lg bg-[#5a6f4e] px-3 py-1 text-xs text-white hover:bg-[#4c5f42]"
                    >
                      采纳
                    </button>
                    <button
                      onClick={() => decide(p.id, false)}
                      className="rounded-lg border border-line px-3 py-1 text-xs text-ink-soft hover:border-ink-soft"
                    >
                      拒绝
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      <ul className="space-y-2">
        {shown.map((c) =>
          editingId === c.id ? (
            <li key={c.id} className="rounded-lg border border-seal/40 bg-card p-4 paper-shadow">
              <textarea
                value={draft.front}
                onChange={(e) => setDraft({ ...draft, front: e.target.value })}
                rows={2}
                className="w-full rounded-md border border-line bg-paper px-3 py-2 text-sm"
                placeholder="正面（问题）"
              />
              <textarea
                value={draft.back}
                onChange={(e) => setDraft({ ...draft, back: e.target.value })}
                rows={5}
                className="mt-2 w-full rounded-md border border-line bg-paper px-3 py-2 text-sm"
                placeholder="背面（答案，支持 Markdown）"
              />
              <div className="mt-2 flex items-center gap-2">
                <input
                  value={draft.category}
                  onChange={(e) => setDraft({ ...draft, category: e.target.value })}
                  className="w-40 rounded-md border border-line bg-paper px-3 py-1.5 text-xs"
                  placeholder="分类"
                />
                <button
                  onClick={save}
                  disabled={saving}
                  className="rounded-lg bg-ink px-4 py-1.5 text-xs text-paper hover:bg-ink/85 disabled:opacity-50"
                >
                  {saving ? "保存中…" : "保存"}
                </button>
                <button
                  onClick={() => setEditingId("")}
                  className="rounded-lg border border-line px-4 py-1.5 text-xs text-ink-soft hover:border-ink-soft"
                >
                  取消
                </button>
              </div>
            </li>
          ) : (
            <li
              key={c.id}
              className="group flex items-start justify-between rounded-lg border border-line bg-card px-4 py-3 paper-shadow"
            >
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{c.front}</p>
                <p className="mt-0.5 truncate text-xs text-faint">{c.back}</p>
              </div>
              <div className="ml-4 flex shrink-0 items-center gap-2">
                {c.category && (
                  <span className="rounded-md bg-[#5a6f4e]/10 px-2 py-0.5 text-xs text-[#5a6f4e]">
                    {c.category}
                  </span>
                )}
                <button
                  onClick={() => startEdit(c)}
                  className="text-xs text-faint opacity-0 transition-opacity hover:text-ink group-hover:opacity-100"
                >
                  编辑
                </button>
                <button
                  onClick={() => remove(c)}
                  className="text-xs text-faint opacity-0 transition-opacity hover:text-seal group-hover:opacity-100"
                >
                  删除
                </button>
              </div>
            </li>
          )
        )}
        {shown.length === 0 && (
          <li className="py-8 text-center text-xs text-faint">
            {cards.length === 0 ? "还没有卡片——先去文档页生成卡片吧" : "该分类下暂无卡片"}
          </li>
        )}
      </ul>
    </div>
  );
}
