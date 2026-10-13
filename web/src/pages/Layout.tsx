import { useEffect, useState } from "react";
import { NavLink, Outlet, useNavigate, useParams } from "react-router-dom";
import { api, Library } from "../api/client";

// 全局布局：左侧知识库列表（多库切换）+ 顶部栏。
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
    <div className="flex h-screen bg-stone-50 text-stone-800">
      {/* 侧栏：多库切换 */}
      <aside className="w-60 shrink-0 border-r border-stone-200 bg-white flex flex-col">
        <div className="px-4 py-4 border-b border-stone-100">
          <h1 className="text-lg font-bold tracking-wide">复磐 <span className="text-stone-400 text-xs font-normal">FuPan</span></h1>
          <p className="text-xs text-stone-400 mt-0.5">把笔记变成会安排复习的记忆系统</p>
        </div>
        <nav className="flex-1 overflow-y-auto py-2">
          {libs.map((l) => (
            <NavLink
              key={l.id}
              to={`/library/${l.id}`}
              className={({ isActive }) =>
                `block px-4 py-2 text-sm truncate ${isActive || libId === l.id ? "bg-stone-100 font-medium text-stone-900" : "text-stone-600 hover:bg-stone-50"}`
              }
            >
              📚 {l.name}
            </NavLink>
          ))}
          {libs.length === 0 && (
            <p className="px-4 py-3 text-xs text-stone-400">还没有知识库，先建一个吧</p>
          )}
        </nav>
        <div className="p-3 border-t border-stone-100 space-y-2">
          {creating ? (
            <div className="flex gap-1">
              <input
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && create()}
                placeholder="库名，如：考研库"
                className="w-full rounded border border-stone-300 px-2 py-1.5 text-sm focus:outline-none focus:border-stone-500"
              />
              <button onClick={create} className="rounded bg-stone-800 px-2 text-sm text-white">✓</button>
            </div>
          ) : (
            <button
              onClick={() => setCreating(true)}
              className="w-full rounded-md border border-stone-300 py-1.5 text-sm text-stone-600 hover:bg-stone-50"
            >
              ＋ 新建知识库
            </button>
          )}
          <NavLink
            to="/settings"
            className={({ isActive }) =>
              `block text-center rounded-md py-1.5 text-sm ${isActive ? "bg-stone-100 text-stone-900" : "text-stone-500 hover:bg-stone-50"}`
            }
          >
            ⚙️ 设置
          </NavLink>
        </div>
      </aside>

      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
