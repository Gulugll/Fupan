import { useEffect, useState } from "react";

// 主题三态：跟随系统 / 白天（纸感书房）/ 夜间（深色专注）。
// 持久化在 localStorage；<html data-theme> 驱动 CSS 变量切换。
export type ThemeMode = "system" | "light" | "dark";

const KEY = "fupan.theme";

export function getMode(): ThemeMode {
  const v = localStorage.getItem(KEY);
  return v === "light" || v === "dark" ? v : "system";
}

function resolve(mode: ThemeMode): "light" | "dark" {
  if (mode !== "system") return mode;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function applyTheme(mode: ThemeMode) {
  document.documentElement.dataset.theme = resolve(mode);
}

export function setMode(mode: ThemeMode) {
  localStorage.setItem(KEY, mode);
  applyTheme(mode);
}

// 应用启动时调用一次（main.tsx），避免首屏闪烁并监听系统主题变化
export function initTheme() {
  applyTheme(getMode());
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    if (getMode() === "system") applyTheme("system");
  });
}

export function useThemeMode(): [ThemeMode, (m: ThemeMode) => void] {
  const [mode, setState] = useState<ThemeMode>(getMode());
  useEffect(() => applyTheme(mode), [mode]);
  const change = (m: ThemeMode) => {
    setMode(m);
    setState(m);
  };
  return [mode, change];
}
