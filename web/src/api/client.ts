// 统一 API 客户端：fetch 封装 + 类型定义（文案走 i18n key，界面中文优先）。

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

async function handle<T>(resp: Response): Promise<T> {
  if (resp.status === 204) return undefined as T;
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error(body.error ?? `请求失败 (${resp.status})`);
  }
  return body as T;
}

export const api = {
  listLibraries: () =>
    fetch("/api/libraries").then(handle<Library[]>),
  createLibrary: (name: string, description: string) =>
    fetch("/api/libraries", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, description }),
    }).then(handle<Library>),
  deleteLibrary: (id: string) =>
    fetch(`/api/libraries/${id}`, { method: "DELETE" }).then(handle<void>),

  listDocuments: (libId: string) =>
    fetch(`/api/libraries/${libId}/documents`).then(handle<DocumentSummary[]>),
  uploadDocument: (libId: string, file: File) => {
    const form = new FormData();
    form.append("file", file);
    return fetch(`/api/libraries/${libId}/documents`, {
      method: "POST",
      body: form,
    }).then(handle<{ id: string; title: string; chunk_count: number }>);
  },
  getDocument: (docId: string) =>
    fetch(`/api/documents/${docId}`).then(handle<{ document: { id: string; title: string; format: string; library_id: string }; chunks: Chunk[] }>),
  deleteDocument: (docId: string) =>
    fetch(`/api/documents/${docId}`, { method: "DELETE" }).then(handle<void>),

  previewCards: (libraryId: string, chunkIds: string[]) =>
    fetch("/api/cardgen/preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ library_id: libraryId, chunk_ids: chunkIds }),
    }).then(handle<{ cards: PreviewCard[]; errors: string[] }>),
  commitCards: (libraryId: string, cards: { front: string; back: string; source_chunk_id: string }[]) =>
    fetch("/api/cardgen/commit", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ library_id: libraryId, cards }),
    }).then(handle<{ count: number; cards: unknown[] }>),

  reviewQueue: (libId: string) =>
    fetch(`/api/libraries/${libId}/review/queue`).then(handle<{ items: QueueItem[] }>),
  reviewCard: (cardId: string, rating: number) =>
    fetch(`/api/cards/${cardId}/review`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ rating }),
    }).then(handle<{ schedule: { state: string; due: string } }>),
  cardSource: (cardId: string) =>
    fetch(`/api/cards/${cardId}/source`).then(handle<SourceInfo>),

  getSettings: () => fetch("/api/settings").then(handle<Record<string, string>>),
  putSettings: (m: Record<string, string>) =>
    fetch("/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(m),
    }).then(handle<{ ok: boolean }>),
};
