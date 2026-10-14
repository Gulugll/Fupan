import { useEffect, useState } from "react";
import { api } from "../api/client";

// 服务商预设：一键填充 Base URL 与默认模型（OpenAI 兼容协议通用）。
const CHAT_PRESETS: Record<string, { base: string; model: string; key?: boolean }> = {
  deepseek: { base: "https://api.deepseek.com", model: "deepseek-chat", key: true },
  zhipu: { base: "https://open.bigmodel.cn/api/paas/v4", model: "glm-4", key: true },
  openai: { base: "https://api.openai.com/v1", model: "gpt-4o-mini", key: true },
  ollama: { base: "http://localhost:11434/v1", model: "qwen3:8b" },
  custom: { base: "", model: "" },
};

const EMBED_PRESETS: Record<string, { base: string; model: string; dim: string }> = {
  follow: { base: "", model: "embedding-3", dim: "2048" }, // 跟随对话端点（智谱）
  ollama: { base: "http://localhost:11434/v1", model: "bge-m3", dim: "1024" },
  custom: { base: "", model: "", dim: "" },
};

// 设置页：模型服务商（对话 + 嵌入独立配置）与复习参数。
export default function Settings() {
  const [s, setS] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");
  const [chatPreset, setChatPreset] = useState("custom");
  const [embedPreset, setEmbedPreset] = useState("custom");
  const [testResult, setTestResult] = useState<{ chat?: string; embed?: string }>({});
  const [testing, setTesting] = useState<"chat" | "embed" | null>(null);

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

  const presetRow = (
    presets: Record<string, { label: string }>,
    current: string,
    onPick: (k: string) => void
  ) => (
    <div className="flex flex-wrap gap-1.5">
      {Object.entries(presets).map(([k, v]) => (
        <button
          key={k}
          onClick={() => onPick(k)}
          className={`rounded-full border px-3 py-1 text-xs transition-colors ${
            current === k
              ? "border-stone-800 bg-stone-800 text-white"
              : "border-stone-300 bg-white text-stone-600 hover:border-stone-500"
          }`}
        >
          {v.label}
        </button>
      ))}
    </div>
  );

  const applyChatPreset = (k: string) => {
    setChatPreset(k);
    const p = CHAT_PRESETS[k];
    if (!p) return;
    setS((prev) => ({
      ...prev,
      ...(p.base !== "" ? { "llm.base_url": p.base } : {}),
      ...(p.model !== "" ? { "llm.chat_model": p.model } : {}),
    }));
  };

  const applyEmbedPreset = (k: string) => {
    setEmbedPreset(k);
    const p = EMBED_PRESETS[k];
    if (!p) return;
    setS((prev) => ({
      ...prev,
      ...(p.base !== "" ? { "embed.base_url": p.base } : { "embed.base_url": "" }),
      ...(p.model !== "" ? { "llm.embed_model": p.model } : {}),
      ...(p.dim !== "" ? { "embed.dim": p.dim } : {}),
    }));
  };

  const test = async (target: "chat" | "embed") => {
    setTesting(target);
    setTestResult((t) => ({ ...t, [target]: "" }));
    try {
      await api.putSettings(s); // 先保存再测，测的就是已存配置
      const r = await api.testSettings(target);
      setTestResult((t) => ({ ...t, [target]: `✓ ${r.detail ?? "连接正常"}` }));
    } catch (e: unknown) {
      setTestResult((t) => ({ ...t, [target]: `✗ ${(e as Error).message}` }));
    } finally {
      setTesting(null);
    }
  };

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
        <h3 className="text-sm font-bold text-stone-700">对话模型</h3>
        {presetRow(
          {
            deepseek: { label: "DeepSeek" },
            zhipu: { label: "智谱" },
            openai: { label: "OpenAI" },
            ollama: { label: "Ollama 本地" },
            custom: { label: "自定义" },
          },
          chatPreset,
          applyChatPreset
        )}
        {field("llm.base_url", "Base URL（OpenAI 兼容）", "https://api.deepseek.com")}
        {field("llm.api_key", "API Key（Ollama 本地无需填写）", "sk-…", "password")}
        {field("llm.chat_model", "对话模型", "deepseek-chat")}
        <button
          onClick={() => test("chat")}
          disabled={testing === "chat"}
          className="rounded-md border border-stone-300 px-3 py-1.5 text-xs text-stone-700 hover:border-stone-500 disabled:opacity-50"
        >
          {testing === "chat" ? "测试中…" : "测试对话连接"}
        </button>
        {testResult.chat && (
          <p className={`text-xs ${testResult.chat.startsWith("✓") ? "text-green-700" : "text-red-600"}`}>
            {testResult.chat}
          </p>
        )}
      </section>

      <section className="mt-4 space-y-4 rounded-xl border border-stone-200 bg-white p-5">
        <h3 className="text-sm font-bold text-stone-700">嵌入模型（向量化，可独立于对话服务商）</h3>
        {presetRow(
          {
            follow: { label: "跟随对话端点" },
            ollama: { label: "Ollama bge-m3" },
            custom: { label: "自定义" },
          },
          embedPreset,
          applyEmbedPreset
        )}
        {field("embed.base_url", "嵌入 Base URL（留空跟随对话端点）", "http://localhost:11434/v1")}
        {field("embed.api_key", "嵌入 API Key（留空跟随对话 Key）", "sk-…", "password")}
        {field("llm.embed_model", "嵌入模型", "bge-m3")}
        {field("embed.dim", "向量维度（须与嵌入模型一致，改后需重启）", "1024")}
        <button
          onClick={() => test("embed")}
          disabled={testing === "embed"}
          className="rounded-md border border-stone-300 px-3 py-1.5 text-xs text-stone-700 hover:border-stone-500 disabled:opacity-50"
        >
          {testing === "embed" ? "测试中…" : "测试嵌入连接"}
        </button>
        {testResult.embed && (
          <p className={`text-xs ${testResult.embed.startsWith("✓") ? "text-green-700" : "text-red-600"}`}>
            {testResult.embed}
          </p>
        )}
        <p className="text-xs text-stone-400">
          推荐：对话走 DeepSeek / 智谱云端，嵌入走 Ollama bge-m3 本地（数据不出机器）。维度改动需重启服务并清理旧向量。
        </p>
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
