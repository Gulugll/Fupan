import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, Library } from "../api/client";

// 首页：库列表概览（侧栏已含切换，这里给出引导与入口）。
export default function LibraryHome() {
  const [libs, setLibs] = useState<Library[]>([]);
  const navigate = useNavigate();

  useEffect(() => {
    api.listLibraries().then((ls) => {
      setLibs(ls);
      if (ls.length > 0) navigate(`/library/${ls[0].id}`, { replace: true });
    }).catch(() => {});
  }, [navigate]);

  return (
    <div className="p-10 max-w-xl mx-auto text-center pt-24">
      <div className="text-6xl mb-6">磐</div>
      <h2 className="text-2xl font-bold mb-2">欢迎来到复磐</h2>
      <p className="text-stone-500 text-sm leading-6">
        1️⃣ 在左侧新建一个知识库<br />
        2️⃣ 上传 Markdown / TXT 笔记 → 自动切片入库<br />
        3️⃣ 勾选小节生成复习卡片 → 开始复习<br />
        每张卡片都能溯源回原文。忘记的，推上山；记住的，铸成磐石。
      </p>
      {libs.length === 0 && (
        <p className="mt-8 text-xs text-stone-400">左侧「＋ 新建知识库」开始</p>
      )}
      <Link to="/settings" className="mt-6 inline-block text-xs text-stone-400 hover:text-stone-600 underline">
        首次使用？先到设置页配置 LLM API Key
      </Link>
    </div>
  );
}
