import { useEffect, useState } from "react";
import { NavLink, Outlet, useNavigate, useParams } from "react-router-dom";
import { api, Library, Me } from "../api/client";
import { useThemeMode } from "../theme";

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
  const [me, setMe] = useState<Me | null>(null);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const navigate = useNavigate();
  const { libId } = useParams();
  const [themeMode, setThemeMode] = useThemeMode();

  const load = () => api.listLibraries().then(setLibs).catch(() => {});
  useEffect(() => {
    load();
    api.me().then(setMe).catch(() => {});
  }, []);

  const logout = async () => {
    await api.logout();
    window.location.assign("/auth/logged-out");
  };

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
      <aside className="flex w-60 shrink-0 flex-col border-r border-line bg-side">
        <NavLink to="/" className="flex items-center gap-2.5 px-4 pb-4 pt-5">
          <SealMark />
          <div>
            <h1 className="font-display text-lg font-bold tracking-wide leading-tight">复磐</h1>
            <p className="text-[11px] text-faint">把笔记变成记忆的磐石</p>
          </div>
        </NavLink>

        <nav className="mx-2 mb-2 space-y-0.5">
          <NavLink
            to="/"
            end
            className={({ isActive }) =>
              `block rounded-md px-3 py-2 text-sm transition-colors ${
                isActive ? "bg-accent-soft font-medium text-accent" : "text-ink-soft hover:bg-hover"
              }`
            }
          >
            ⌂ 首页总览
          </NavLink>
        </nav>

        <p className="px-4 pb-1 pt-2 text-[11px] tracking-widest text-faint">知 识 库</p>
        <nav className="flex-1 overflow-y-auto pb-2">
          {libs.map((l) => (
            <NavLink
              key={l.id}
              to={`/library/${l.id}`}
              className={({ isActive }) =>
                `mx-2 block truncate rounded-md px-3 py-2 text-sm transition-colors ${
                  isActive || libId === l.id
                    ? "bg-card font-medium text-accent paper-shadow"
                    : "text-ink-soft hover:bg-hover"
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
                isActive ? "bg-hover text-ink" : "text-faint hover:text-ink-soft"
              }`
            }
          >
            设置
          </NavLink>
          <div className="flex items-center justify-between rounded-md border border-line px-2.5 py-1.5">
            <span className="text-xs text-faint">外观</span>
            <select
              value={themeMode}
              onChange={(e) => setThemeMode(e.target.value as typeof themeMode)}
              className="bg-transparent text-xs text-ink-soft focus:outline-none"
            >
              <option value="system">跟随系统</option>
              <option value="light">白天 · 纸感</option>
              <option value="dark">夜间 · 深色</option>
            </select>
          </div>
          {me && (
            <div className="space-y-1.5 pt-2 text-center">
              <p className="truncate text-xs text-ink-soft" title={me.display_name}>
                当前用户：{me.display_name}
              </p>
              {!me.vector_enabled && (
                <p className="text-[11px] leading-4 text-faint">向量功能已禁用，语义检索与嵌入测试不可用</p>
              )}
              {me.id !== "local" && (
                <button
                  onClick={() => void logout()}
                  className="w-full rounded-md py-1 text-xs text-faint hover:text-ink"
                >
                  退出登录
                </button>
              )}
            </div>
          )}
        </div>
      </aside>

      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
