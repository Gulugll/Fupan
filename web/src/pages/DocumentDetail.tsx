import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { api, Chunk, PreviewCard } from "../api/client";

// 文档详情：chunk 树（父块分组）→ 勾选子块小节 → 卡片预览勾选 → 确认入库。
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
  const parentOf = (c: Chunk) => parents.find((p) => p.id === c.parent_id);

  const toggle = (id: string) => {
    setSelected((s) => {
      const n = new Set(s);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });
  };

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
    setMessage(`已入库 ${res.count} 张卡片，可在「开始复习」中见到它们。`);
    setPreviews([]);
    setSelected(new Set());
  };

  return (
    <div className="p-8 max-w-3xl mx-auto">
      <h2 className="text-xl font-bold mb-1">{doc?.title ?? "…"}</h2>
      <p className="text-xs text-stone-400 mb-6">{children.length} 个切片 · 勾选小节后生成卡片</p>

      {/* chunk 树：父块为组标题，子块可勾选 */}
      <div className="space-y-4">
        {parents.map((p) => (
          <section key={p.id} className="rounded-lg border border-stone-200 bg-white">
            <header className="border-b border-stone-100 px-4 py-2">
              <span className="text-xs font-medium text-stone-500">📁 {p.title_path || "（无标题路径）"}</span>
            </header>
            <div className="divide-y divide-stone-50">
              {children.filter((c) => c.parent_id === p.id).map((c) => (
                <label key={c.id} className="flex gap-3 px-4 py-3 cursor-pointer hover:bg-stone-50">
                  <input
                    type="checkbox"
                    checked={selected.has(c.id)}
                    onChange={() => toggle(c.id)}
                    className="mt-1"
                  />
                  <div className="min-w-0">
                    <p className="text-sm whitespace-pre-wrap line-clamp-6">{c.content}</p>
                    {parentOf(c) && (
                      <p className="mt-1 text-xs text-stone-400">来源：{c.title_path}</p>
                    )}
                  </div>
                </label>
              ))}
            </div>
          </section>
        ))}
      </div>

      <div className="sticky bottom-4 mt-6 flex items-center gap-3 rounded-lg border border-stone-200 bg-white p-3 shadow-sm">
        <span className="text-xs text-stone-500">已选 {selected.size} 个小节</span>
        <button
          onClick={genPreview}
          disabled={selected.size === 0 || previewing}
          className="rounded-md bg-stone-800 px-4 py-1.5 text-sm text-white disabled:opacity-40"
        >
          {previewing ? "生成中……" : "🤖 生成卡片预览"}
        </button>
        {message && <span className="text-sm text-green-700">{message}</span>}
      </div>
      {errors.length > 0 && (
        <ul className="mt-3 text-xs text-red-600 space-y-1">
          {errors.map((e, i) => <li key={i}>⚠️ {e}</li>)}
        </ul>
      )}

      {/* 卡片预览：勾选后入库 */}
      {previews.length > 0 && (
        <div className="mt-8">
          <h3 className="text-lg font-bold mb-3">卡片预览（{previews.length} 张，勾选要入库的）</h3>
          <ul className="space-y-3">
            {previews.map((p, i) => (
              <li key={i} className="rounded-lg border border-stone-200 bg-white p-4">
                <label className="flex gap-3 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={chosen.has(i)}
                    onChange={() => setChosen((s) => {
                      const n = new Set(s);
                      n.has(i) ? n.delete(i) : n.add(i);
                      return n;
                    })}
                  />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium">{p.front}</p>
                    <p className="mt-1 text-sm text-stone-600">{p.back}</p>
                    <p className="mt-2 text-xs text-stone-400">📄 {p.doc_title} › {p.title_path}</p>
                  </div>
                </label>
              </li>
            ))}
          </ul>
          <button
            onClick={commit}
            disabled={chosen.size === 0}
            className="mt-4 rounded-md bg-green-700 px-4 py-2 text-sm text-white disabled:opacity-40"
          >
            ✓ 确认入库（{chosen.size} 张）
          </button>
        </div>
      )}
    </div>
  );
}
