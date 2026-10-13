import { useEffect, useState } from "react";
import { api } from "../api/client";

// 设置页：LLM（OpenAI 兼容）与复习参数。Key 用户自配，存本地 SQLite。
export default function Settings() {
  const [s, setS] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    api.getSettings().then(setS).catch((e) => setError(e.message));
  }, []);

  const field = (key: string, label: string, placeholder = "", type = "text") => (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-stone-500">{label}</span>
      <input
        type={type}
        value={s[key] ?? ""}
        placeholder={placeholder}
        onChange={(e) => setS({ ...s, [key]: e.target.value })}
        className="w-full rounded-md border border-stone-300 px-3 py-2 text-sm focus:border-stone-500 focus:outline-none"
      />
    </label>
  );

  const save = async () => {
    setError("");
    try {
      await api.putSettings(s);
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (e: unknown) {
      setError((e as Error).message);
    }
  };

  return (
    <div className="p-8 max-w-lg mx-auto">
      <h2 className="text-xl font-bold mb-6">设置</h2>

      <section className="space-y-4 rounded-xl border border-stone-200 bg-white p-5">
        <h3 className="text-sm font-bold text-stone-700">LLM（OpenAI 兼容协议）</h3>
        {field("llm.base_url", "Base URL", "https://open.bigmodel.cn/api/paas/v4")}
        {field("llm.api_key", "API Key（智谱 / OpenAI / DeepSeek / Ollama）", "sk-…", "password")}
        {field("llm.chat_model", "对话模型", "glm-4")}
        {field("llm.embed_model", "嵌入模型", "embedding-3")}
        <p className="text-xs text-stone-400">本地版 Key 仅存于本机数据库。使用 Ollama 时填 http://localhost:11434/v4 等兼容端点。</p>
      </section>

      <section className="mt-4 space-y-4 rounded-xl border border-stone-200 bg-white p-5">
        <h3 className="text-sm font-bold text-stone-700">复习参数</h3>
        {field("fsrs.retention", "目标记忆保持率（0.8–0.98）", "0.9")}
        {field("review.daily_new_limit", "每日新卡上限", "20")}
      </section>

      <button
        onClick={save}
        className="mt-6 rounded-md bg-stone-800 px-5 py-2 text-sm text-white hover:bg-stone-700"
      >
        {saved ? "✓ 已保存" : "保存设置"}
      </button>
      {error && <p className="mt-3 text-sm text-red-600">{error}</p>}
    </div>
  );
}
