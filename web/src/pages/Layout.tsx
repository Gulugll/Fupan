import { useEffect, useState } from "react";
import { NavLink, Outlet, useNavigate, useParams } from "react-router-dom";
import { api, Library } from "../api/client";

// 品牌印章：方形朱红印章内嵌「磐」字（视觉锚点，全站唯一装饰重笔）。
function SealMark() {
  return (
    <span className="inline-flex h-8 w-8 items-center justify-center rounded-[4px] bg-seal font-display text-lg leading-none text-[#f8f3e7] shadow-[inset_0_0_0_1.5px_rgba(248,243,231,0.55)]">
      磐
    </span>
  );
}

// 全局布局：左侧知识库目录（纸质书脊感）+ 内容区。
export default function Layout() {
  const [libs, setLibs] = useState<Library[]>([]);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const navigate = useNavigate();
  const { libId } = useParams();

  const load = () => api.listLibraries().then(setLibs).catch(() => {});
  useEffect(() => {
    load();
  }, []);

  const create = async () => {
    if (!name.trim()) return;
    const lib = await api.createLibrary(name.trim(), "");
    setName("");
    setCreating(false);
    await load();
    navigate(`/library/${lib.id}`);
  };

  return (
    <div className="flex h-screen bg-paper text-ink">
      {/* 侧栏：知识库目录 */}
      <aside className="flex w-60 shrink-0 flex-col border-r border-line bg-[#f1ede3]">
        <div className="flex items-center gap-2.5 px-4 pb-4 pt-5">
          <SealMark />
          <div>
            <h1 className="font-display text-lg font-bold tracking-wide leading-tight">复磐</h1>
            <p className="text-[11px] text-faint">把笔记变成记忆的磐石</p>
          </div>
        </div>

        <p className="px-4 pb-1 pt-2 text-[11px] tracking-widest text-faint">知 识 库</p>
        <nav className="flex-1 overflow-y-auto pb-2">
          {libs.map((l) => (
            <NavLink
              key={l.id}
              to={`/library/${l.id}`}
              className={({ isActive }) =>
                `mx-2 block truncate rounded-md px-3 py-2 text-sm transition-colors ${
                  isActive || libId === l.id
                    ? "bg-card font-medium text-ink paper-shadow"
                    : "text-ink-soft hover:bg-[#e9e4d7]"
                }`
              }
            >
              {l.name}
            </NavLink>
          ))}
          {libs.length === 0 && (
            <p className="px-4 py-3 text-xs text-faint">还没有知识库，先建一个吧</p>
          )}
        </nav>

        <div className="space-y-1.5 border-t border-line p-3">
          {creating ? (
            <div className="flex gap-1.5">
              <input
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") create();
                  if (e.key === "Escape") setCreating(false);
                }}
                placeholder="库名，如：考研库"
                className="w-full rounded-md border border-line bg-card px-2.5 py-1.5 text-sm focus:border-ink-soft focus:outline-none"
              />
              <button
                onClick={create}
                className="rounded-md bg-ink px-2.5 text-sm text-paper hover:bg-ink/85"
              >
                建
              </button>
            </div>
          ) : (
            <button
              onClick={() => setCreating(true)}
              className="w-full rounded-md border border-line py-1.5 text-sm text-ink-soft transition-colors hover:border-ink-soft hover:text-ink"
            >
              ＋ 新建知识库
            </button>
          )}
          <NavLink
            to="/settings"
            className={({ isActive }) =>
              `block rounded-md py-1.5 text-center text-sm ${
                isActive ? "bg-[#e9e4d7] text-ink" : "text-faint hover:text-ink-soft"
              }`
            }
          >
            设置
          </NavLink>
        </div>
      </aside>

      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
