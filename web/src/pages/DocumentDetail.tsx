import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { api, Chunk, PreviewCard } from "../api/client";
import Markdown from "../components/Markdown";

// 文档详情：chunk 按父块分组 → 勾选小节（支持全局/分组全选）→ 卡片预览勾选 → 入库。
// 勾选只认复选框本身（一处控件），行与标题不再整行可点。
export default function DocumentDetail() {
  const { docId } = useParams();
  const [doc, setDoc] = useState<{ id: string; title: string; format: string; library_id: string } | null>(null);
  const [chunks, setChunks] = useState<Chunk[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [previewing, setPreviewing] = useState(false);
  const [previews, setPreviews] = useState<PreviewCard[]>([]);
  const [chosen, setChosen] = useState<Set<number>>(new Set());
  const [errors, setErrors] = useState<string[]>([]);
  const [message, setMessage] = useState("");

  useEffect(() => {
    if (!docId) return;
    api.getDocument(docId).then((d) => {
      setDoc(d.document);
      setChunks(d.chunks);
    });
  }, [docId]);

  const children = chunks.filter((c) => c.kind === "child");
  const parents = chunks.filter((c) => c.kind === "parent");

  // 父块 → 其子块 的分组视图。
  const groups = useMemo(
    () => parents.map((p) => ({ parent: p, items: children.filter((c) => c.parent_id === p.id) })),
    [parents, children]
  );

  const toggle = (id: string) => {
    setSelected((s) => {
      const n = new Set(s);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });
  };

  // 全局全选/清空（覆盖所有小节）。
  const toggleAll = () => {
    setSelected((s) => (s.size === children.length ? new Set() : new Set(children.map((c) => c.id))));
  };
  const allSelected = children.length > 0 && selected.size === children.length;

  const genPreview = async () => {
    if (!doc?.library_id || selected.size === 0) return;
    setPreviewing(true);
    setErrors([]);
    setMessage("");
    try {
      const res = await api.previewCards(doc.library_id, [...selected]);
      setPreviews(res.cards);
      setErrors(res.errors);
      setChosen(new Set(res.cards.map((_, i) => i)));
      if (res.cards.length > 0) {
        setTimeout(
          () => document.getElementById("preview-list")?.scrollIntoView({ behavior: "smooth" }),
          100
        );
      }
    } catch (e: unknown) {
      setErrors([(e as Error).message]);
    } finally {
      setPreviewing(false);
    }
  };

  const commit = async () => {
    if (!doc?.library_id) return;
    const cards = [...chosen].map((i) => previews[i]).map((p) => ({
      front: p.front, back: p.back, source_chunk_id: p.source_chunk_id,
    }));
    const res = await api.commitCards(doc.library_id, cards);
    setMessage(`已入库 ${res.count} 张卡片，可到「开始复习」中见到它们。`);
    setPreviews([]);
    setSelected(new Set());
  };

  return (
    <div className="mx-auto max-w-3xl px-8 py-8">
      <h2 className="font-display text-2xl font-bold">{doc?.title ?? "…"}</h2>
      <p className="mt-1 mb-4 text-xs text-faint">
        {groups.length} 个章节 · {children.length} 个切片 · 勾选小节后生成卡片
        <button
          onClick={toggleAll}
          className="ml-3 rounded-md border border-line px-2 py-0.5 text-[11px] text-ink-soft transition-colors hover:border-ink-soft hover:text-ink"
        >
          {allSelected ? "全不选" : "全选小节"}
        </button>
      </p>

      {/* chunk 分组：组头仅作标题与进度展示，勾选只在小节行（一处控件） */}
      <div className="space-y-4">
        {groups.map((g) => {
          const count = g.items.filter((c) => selected.has(c.id)).length;
          return (
            <section key={g.parent.id} className="overflow-hidden rounded-xl border border-line bg-card paper-shadow">
              <div className="flex items-center gap-3 border-b border-line bg-[#faf7f0] px-4 py-2.5">
                <span className="min-w-0 flex-1 truncate font-display text-sm font-medium">
                  {g.parent.title_path || "（无标题路径）"}
                </span>
                <span className="text-[11px] text-faint">
                  {count > 0 ? `${count}/${g.items.length} 已选` : `${g.items.length} 小节`}
                </span>
              </div>
              <div className="divide-y divide-[#f0ebdf]">
                {g.items.map((c) => (
                  <div key={c.id} className="flex gap-3 px-4 py-3">
                    <input
                      type="checkbox"
                      checked={selected.has(c.id)}
                      onChange={() => toggle(c.id)}
                      className="mt-1 accent-seal"
                    />
                    <p className="min-w-0 flex-1 whitespace-pre-wrap text-sm leading-6 line-clamp-6">
                      {c.content.replace(/^#{1,6}\s+[^\n]*\n+/gm, "")}
                    </p>
                  </div>
                ))}
              </div>
            </section>
          );
        })}
      </div>

      {/* 底部操作条 */}
      <div className="sticky bottom-4 mt-6 flex items-center gap-3 rounded-xl border border-line bg-card p-3 paper-shadow">
        <span className="text-xs text-ink-soft">已选 {selected.size} 个小节</span>
        <button
          onClick={genPreview}
          disabled={selected.size === 0 || previewing}
          className="rounded-lg bg-ink px-4 py-1.5 text-sm text-paper transition-colors hover:bg-ink/85 disabled:opacity-40"
        >
          {previewing ? (
            <span className="inline-flex items-center gap-2">
              <span className="h-3 w-3 animate-spin rounded-full border-[1.5px] border-paper/40 border-t-paper" />
              正在生成，通常需要几秒……
            </span>
          ) : (
            "生成卡片预览"
          )}
        </button>
        {message && <span className="text-sm text-[#5a6f4e]">{message}</span>}
      </div>
      {errors.length > 0 && (
        <ul className="mt-3 space-y-1 text-xs text-seal">
          {errors.map((e, i) => <li key={i}>{e}</li>)}
        </ul>
      )}

      {/* 卡片预览：闪卡样式，勾选后入库 */}
      {previews.length > 0 && (
        <div id="preview-list" className="mt-10 scroll-mt-6">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="font-display text-lg font-bold">卡片预览 · {previews.length} 张</h3>
            <div className="flex gap-2 text-xs">
              <button
                onClick={() => setChosen(new Set(previews.map((_, i) => i)))}
                className="rounded-md border border-line px-2.5 py-1 text-ink-soft hover:border-ink-soft hover:text-ink"
              >
                全选
              </button>
              <button
                onClick={() => setChosen(new Set())}
                className="rounded-md border border-line px-2.5 py-1 text-ink-soft hover:border-ink-soft hover:text-ink"
              >
                清空
              </button>
            </div>
          </div>
          <ul className="space-y-3">
            {previews.map((p, i) => (
              <li
                key={i}
                className={`rounded-xl border bg-card p-4 transition-colors ${
                  chosen.has(i) ? "border-seal/50" : "border-line opacity-60"
                }`}
              >
                <div className="flex gap-3">
                  <input
                    type="checkbox"
                    checked={chosen.has(i)}
                    onChange={() => setChosen((s) => {
                      const n = new Set(s);
                      n.has(i) ? n.delete(i) : n.add(i);
                      return n;
                    })}
                    className="mt-1.5 accent-seal"
                  />
                  <div className="min-w-0 flex-1">
                    <p className="text-[11px] tracking-widest text-faint">问</p>
                    <Markdown text={p.front} className="font-display text-[15px] font-medium leading-7" />
                    <p className="mt-2 text-[11px] tracking-widest text-faint">答</p>
                    <Markdown text={p.back} className="text-sm text-ink-soft" />
                    <p className="mt-2.5 border-t border-dashed border-line pt-2 text-xs text-faint">
                      <span className="text-seal">溯源</span> {p.doc_title} › {p.title_path}
                    </p>
                  </div>
                </div>
              </li>
            ))}
          </ul>
          <button
            onClick={commit}
            disabled={chosen.size === 0}
            className="mt-5 rounded-lg bg-[#5a6f4e] px-5 py-2.5 text-sm text-white transition-colors hover:bg-[#4c5f42] disabled:opacity-40"
          >
            入库（{chosen.size} 张）
          </button>
        </div>
      )}
    </div>
  );
}
