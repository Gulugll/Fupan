import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { api, DocumentSummary } from "../api/client";
import MemoryStats from "../components/MemoryStats";

// 库详情：文档列表 + 上传 MD/TXT（拖拽或点选）。
export default function LibraryDetail() {
  const { libId } = useParams();
  const location = useLocation();
  // 一次性 flash 提示（如从文档页入库卡片后跳转过来），读过即清。
  const [flash, setFlash] = useState<string>(
    (location.state as { flash?: string } | null)?.flash ?? ""
  );
  const [docs, setDocs] = useState<DocumentSummary[]>([]);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();

  const load = () =>
    libId && api.listDocuments(libId).then(setDocs).catch((e) => setError(e.message));
  useEffect(() => {
    load();
    // flash 只显示一次：清掉 history state，刷新/前进后退不再重现。
    if ((location.state as { flash?: string } | null)?.flash) {
      window.history.replaceState({}, "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [libId]);

  const upload = async (file: File) => {
    if (!libId) return;
    setUploading(true);
    setError("");
    try {
      const res = await api.uploadDocument(libId, file);
      await load();
      navigate(`/document/${res.id}`);
    } catch (e: unknown) {
      setError((e as Error).message);
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="mx-auto max-w-3xl px-8 py-8">
      <div className="mb-6 flex items-center justify-between">
        <h2 className="font-display text-2xl font-bold">
          文档
          {flash && (
            <span className="ml-3 rounded-md bg-[#5a6f4e]/10 px-2.5 py-1 align-middle text-xs font-normal text-[#5a6f4e]">
              {flash}
            </span>
          )}
        </h2>
        {libId && (
          <Link
            to={`/review/${libId}`}
            className="rounded-lg bg-ink px-4 py-2 text-sm text-paper transition-colors hover:bg-ink/85"
          >
            开始复习
          </Link>
        )}
      </div>

      {/* 记忆统计：今日进度 + 7 日到期分布 + 状态分布 */}
      {libId && <MemoryStats libId={libId} />}

      {/* 上传区 */}
      <div
        onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          const f = e.dataTransfer.files?.[0];
          if (f) upload(f);
        }}
        onClick={() => fileRef.current?.click()}
        className={`cursor-pointer rounded-xl border-2 border-dashed p-10 text-center transition-colors ${
          dragOver
            ? "border-seal bg-[#faf3ec]"
            : "border-line bg-card hover:border-ink-soft"
        }`}
      >
        <input
          ref={fileRef}
          type="file"
          accept=".md,.markdown,.txt"
          className="hidden"
          onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])}
        />
        {uploading ? (
          <p className="inline-flex items-center gap-2 text-sm text-ink-soft">
            <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-line border-t-seal" />
            正在导入并切片……
          </p>
        ) : (
          <>
            <p className="text-sm text-ink">拖拽 <b>.md / .txt</b> 文件到此处，或点击选择</p>
            <p className="mt-1.5 text-xs text-faint">导入后自动按标题树切片，代码块与表格不会拆开</p>
          </>
        )}
      </div>
      {error && <p className="mt-3 text-sm text-seal">{error}</p>}

      {/* 文档列表 */}
      <ul className="mt-6 space-y-2">
        {docs.map((d) => (
          <li
            key={d.id}
            className="group flex items-center justify-between rounded-lg border border-line bg-card px-4 py-3 paper-shadow"
          >
            <Link to={`/document/${d.id}`} className="flex-1">
              <span className="text-sm font-medium group-hover:text-seal">{d.title}</span>
              <span className="ml-3 text-xs text-faint">{d.chunk_count} 个切片</span>
            </Link>
            <button
              onClick={async () => {
                if (confirm(`删除文档「${d.title}」？其卡片与向量将一并清理。`)) {
                  await api.deleteDocument(d.id);
                  load();
                }
              }}
              className="text-xs text-faint opacity-0 transition-opacity hover:text-seal group-hover:opacity-100"
            >
              删除
            </button>
          </li>
        ))}
        {docs.length === 0 && !uploading && (
          <li className="py-6 text-center text-xs text-faint">还没有文档</li>
        )}
      </ul>
    </div>
  );
}
