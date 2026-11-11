// 统一 API 客户端：fetch 封装 + 类型定义（文案走 i18n key，界面中文优先）。

export interface Me {
  id: string;
  display_name: string;
  issuer?: string;
  sub?: string;
  vector_enabled: boolean;
}

export interface Library {
  id: string;
  name: string;
  description: string;
  created_at: string;
}

export interface DocumentSummary {
  id: string;
  title: string;
  format: string;
  chunk_count: number;
  created_at: string;
}

export interface Chunk {
  id: string;
  doc_id: string;
  parent_id: string;
  kind: "parent" | "child";
  title_path: string;
  seq: number;
  content: string;
  char_start: number;
  char_end: number;
}

export interface PreviewCard {
  front: string;
  back: string;
  category?: string;
  source_chunk_id: string;
  title_path: string;
  doc_title: string;
}

export interface QueueItem {
  id: string;
  front: string;
  back: string;
  state: string;
  is_new: boolean;
  due: string;
}

export interface ReviewStats {
  total_cards: number;
  state_counts: Record<string, number>;
  today_new: number;
  today_reviews: number;
  due_now: number;
  forecast: { date: string; count: number }[]; // 今日起 7 天，逾期计入今天
  history: { date: string; reviews: number; good: number }[]; // 近 30 天（空天补零）
}

export interface SourceInfo {
  doc_id: string;
  doc_title: string;
  title_path: string;
  parent_id: string;
  parent_text: string;
  child_text: string;
  char_start: number;
  char_end: number;
}

export interface Card {
  id: string;
  front: string;
  back: string;
  category: string;
  status: string;
  created_at: string;
}

export interface ChatMessage {
  role: "system" | "user" | "assistant" | "tool";
  content: string;
}

export interface ChatResult {
  messages: ChatMessage[];
  suggested_rating: number; // 0 = agent 未建议
}

export interface CardProposal {
  id: string;
  library_id: string;
  card_id: string;
  kind: "edit" | "new";
  payload: string; // JSON: {front,back,category,source_chunk_id?}
  status: "pending" | "applied" | "rejected";
  rationale: string;
  created_at: string;
}

async function handle<T>(resp: Response): Promise<T> {
  if (resp.status === 401) {
    const returnTo = `${window.location.pathname}${window.location.search}${window.location.hash}`;
    window.location.assign(`/auth/login?return_to=${encodeURIComponent(returnTo)}`);
    throw new Error("登录已失效，正在跳转登录页");
  }
  if (resp.status === 204) return undefined as T;
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error(body.error ?? `请求失败 (${resp.status})`);
  }
  return body as T;
}

export const api = {
  me: () => fetch("/api/me").then(handle<Me>),
  logout: () => fetch("/auth/logout", { method: "POST" }).then(handle<void>),

  listLibraries: () =>
    fetch("/api/libraries").then(handle<Library[]>).then((r) => r ?? []),
  createLibrary: (name: string, description: string) =>
    fetch("/api/libraries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, description }),
    }).then(handle<Library>),
  deleteLibrary: (id: string) =>
    fetch(`/api/libraries/${id}`, { method: "DELETE" }).then(handle<void>),

  listDocuments: (libId: string) =>
    fetch(`/api/libraries/${libId}/documents`).then(handle<DocumentSummary[]>).then((r) => r ?? []),
  uploadDocument: (libId: string, file: File) => {
    const form = new FormData();
    form.append("file", file);
    return fetch(`/api/libraries/${libId}/documents`, {
      method: "POST",
      body: form,
    }).then(handle<{ id: string; title: string; chunk_count: number }>);
  },
  getDocument: (docId: string) =>
    fetch(`/api/documents/${docId}`)
      .then(handle<{ document: { id: string; title: string; format: string; library_id: string }; chunks: Chunk[] }>)
      .then((r) => ({ ...r, chunks: r.chunks ?? [] })),
  deleteDocument: (docId: string) =>
    fetch(`/api/documents/${docId}`, { method: "DELETE" }).then(handle<void>),

  previewCards: (libraryId: string, chunkIds: string[]) =>
    fetch("/api/cardgen/preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ library_id: libraryId, chunk_ids: chunkIds }),
    })
      .then(handle<{ cards: PreviewCard[]; errors: string[] }>)
      // Go 空切片会序列化成 null，统一兜底成数组，避免下游 .length/.map 崩溃。
      .then((r) => ({ cards: r.cards ?? [], errors: r.errors ?? [] })),
  commitCards: (libraryId: string, cards: { front: string; back: string; category?: string; source_chunk_id: string }[]) =>
    fetch("/api/cardgen/commit", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ library_id: libraryId, cards }),
    }).then(handle<{ count: number; cards: unknown[] }>),

  reviewQueue: (libId: string, forceNew = false, all = false) => {
    const params = new URLSearchParams();
    if (forceNew) params.set("force_new", "1");
    if (all) params.set("mode", "all");
    const qs = params.toString();
    return fetch(`/api/libraries/${libId}/review/queue${qs ? `?${qs}` : ""}`, { method: "POST" })
      .then(handle<{
        items: QueueItem[];
        stats?: {
          due_count: number;
          new_remaining: number;
          daily_new_used: number;
          daily_new_max: number;
        };
      }>)
      .then((r) => ({ items: r.items ?? [], stats: r.stats ?? null }));
  },
  quiz: (libId: string, category = "", limit = 10) => {
    const params = new URLSearchParams({ limit: String(limit) });
    if (category) params.set("category", category);
    return fetch(`/api/libraries/${libId}/quiz?${params}`)
      .then(handle<Card[]>)
      .then((r) => r ?? []);
  },
  reviewCard: (cardId: string, rating: number) =>
    fetch(`/api/cards/${cardId}/review`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ rating }),
    }).then(handle<{ schedule: { state: string; due: string } }>),
  cardSource: (cardId: string) =>
    fetch(`/api/cards/${cardId}/source`).then(handle<SourceInfo>),

  reviewStats: (libId: string) =>
    fetch(`/api/libraries/${libId}/review/stats`).then(handle<ReviewStats | null>),

  listCards: (libId: string, category = "") =>
    fetch(`/api/libraries/${libId}/cards${category ? `?category=${encodeURIComponent(category)}` : ""}`)
      .then(handle<Card[]>).then((r) => r ?? []),
  updateCard: (id: string, patch: { front?: string; back?: string; category?: string }) =>
    fetch(`/api/cards/${id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    }).then(handle<Card>),
  deleteCard: (id: string) =>
    fetch(`/api/cards/${id}`, { method: "DELETE" }).then(handle<void>),

  agentChat: (libId: string, cardId: string, messages: ChatMessage[]) =>
    fetch("/api/agent/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ library_id: libId, card_id: cardId, messages }),
    }).then(handle<ChatResult>),

  listProposals: (libId: string, status = "pending") =>
    fetch(`/api/libraries/${libId}/proposals?status=${status}`)
      .then(handle<CardProposal[]>)
      .then((r) => r ?? []),
  applyProposal: (id: string) =>
    fetch(`/api/proposals/${id}/apply`, { method: "POST" }).then(handle<CardProposal>),
  rejectProposal: (id: string) =>
    fetch(`/api/proposals/${id}/reject`, { method: "POST" }).then(handle<CardProposal>),

  getSettings: () => fetch("/api/settings").then(handle<Record<string, string>>),
  testSettings: (target: "chat" | "embed" | "search") =>
    fetch("/api/settings/test", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ target }),
    }).then(handle<{ ok: boolean; detail?: string }>),
  putSettings: (m: Record<string, string>) =>
    fetch("/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(m),
    }).then(handle<{ ok: boolean }>),
};
