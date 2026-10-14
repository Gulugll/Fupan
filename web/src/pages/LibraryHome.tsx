import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, Library } from "../api/client";

// 首页：空库引导（有库时自动跳转，极少停留）。
export default function LibraryHome() {
  const [libs, setLibs] = useState<Library[]>([]);
  const navigate = useNavigate();

  useEffect(() => {
    api.listLibraries().then((ls) => {
      setLibs(ls);
      if (ls.length > 0) navigate(`/library/${ls[0].id}`, { replace: true });
    }).catch(() => {});
  }, [navigate]);

  const steps = [
    ["建库", "在左侧新建一个知识库"],
    ["导入", "上传 Markdown / TXT 笔记，自动按标题切片"],
    ["制卡", "勾选小节生成复习卡片"],
    ["复习", "到期即复习，每张卡都能溯源回原文"],
  ];

  return (
    <div className="mx-auto max-w-xl px-6 pt-28 text-center">
      <div className="mx-auto mb-8 flex h-16 w-16 items-center justify-center rounded-md bg-seal font-display text-4xl text-[#f8f3e7] shadow-[inset_0_0_0_2px_rgba(248,243,231,0.5)]">
        磐
      </div>
      <h2 className="font-display text-3xl font-bold">欢迎来到复磐</h2>
      <p className="mt-3 text-sm leading-6 text-ink-soft">
        忘记的，推上山；记住的，铸成磐石。
      </p>

      <ol className="mt-10 space-y-3 text-left">
        {steps.map(([t, d], i) => (
          <li key={t} className="flex items-baseline gap-3 rounded-lg border border-line bg-card px-4 py-3 paper-shadow">
            <span className="font-display text-seal">{["一", "二", "三", "四"][i]}</span>
            <span className="text-sm font-medium">{t}</span>
            <span className="text-sm text-ink-soft">{d}</span>
          </li>
        ))}
      </ol>

      {libs.length === 0 && (
        <p className="mt-8 text-xs text-faint">从左侧「＋ 新建知识库」开始</p>
      )}
      <Link to="/settings" className="mt-6 inline-block text-xs text-faint underline hover:text-ink-soft">
        首次使用？先到设置页配置模型服务
      </Link>
    </div>
  );
}
