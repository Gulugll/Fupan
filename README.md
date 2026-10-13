# 复磐 FuPan

> 把你自己的笔记变成一个会安排复习、能溯源到原文的个人记忆系统。
> 本地优先 · 开源 MIT · 单二进制

**核心闭环**：文档导入 → 按标题树切片 → 向量化入库（Milvus）→ AI 生成问答卡（每张卡可溯源到原文）→ FSRS 遗忘曲线调度复习 → 翻卡自评 → 下次复习时间自动排程。

## 5 分钟快速开始

### 方式一：Docker 一键起（含 Milvus）

```bash
git clone https://github.com/Gulugll/fupan.git && cd fupan
docker compose -f deployments/docker-compose.yml up -d
# 等待 Milvus 健康检查通过（首次约 1 分钟）
open http://localhost:8080
```

### 方式二：本地单二进制（无 Docker）

需要 Go 1.26+ 与 Node 22+：

```bash
make build          # 前端构建 + embed + 编译单二进制 ./fupan
./fupan             # 默认 :8080，Milvus 不可达时向量功能自动降级，其余功能不受影响
open http://localhost:8080
```

### 首次使用

1. 左侧「＋ 新建知识库」建一个库（如「考研库」）；
2. 进入库 → 拖入 `.md` / `.txt` 文档 → 自动切片（Markdown 按标题树、TXT 滑窗，代码块/表格不拆开）；
3. 右上角「⚙️ 设置」配置 LLM API Key（默认智谱 GLM-4 + embedding-3，OpenAI 兼容协议，可换 OpenAI / DeepSeek / Ollama）；
4. 回到文档 → 勾选小节 →「生成卡片预览」→ 勾选确认入库；
5. 「▶ 开始复习」→ 翻卡 → 1–4 自评（1=完全想不起来 2=模糊 3=想起来了 4=秒答）；
6. 卡片右下角「📄 来源徽章」可回跳高亮原文；关掉重开，复习进度都在。

## 架构

```
web (React+Vite+Tailwind)  ──embed──▶  fupan 单二进制 (Go/Gin)
                                          │
              ┌───────────────────────────┼──────────────────┐
        SQLite (Repository 接口)    Milvus (VectorStore 接口)   LLM (OpenAI 兼容协议)
        父块/卡片/调度/复习日志        子块向量（检索用小块）        智谱 GLM-4 + embedding-3
```

- **双粒度索引**：子块（检索单元）进 Milvus，父块（上下文单元）存 SQLite；
- **切片与嵌入解耦**：`Chunker` 接口（`heading` / `fixed` 两个实现），换嵌入模型只需重跑向量化；
- **存储可替换**：Repository 与 VectorStore 均为接口，预留 sqlite-vec 降级与 PostgreSQL 云端迁移。

## 硬约束（设计原则）

| 原则 | 落实 |
|---|---|
| 原文不可变 | chunk content 永不 UPDATE，修订一律版本 INSERT |
| 溯源必填 | `cards.source_chunk_id` NOT NULL 外键，提交时显式校验 |
| 切片与嵌入解耦 | Chunker 接口，策略不绑定模型 |
| 存储可替换 | Repository / VectorStore 接口化 |
| LLM 不绑定厂商 | 全部走 OpenAI 兼容协议，Key 用户自配 |

## 默认参数

目标记忆保持率 0.9 ｜ 每日新卡上限 20 ｜ 每小节 1–3 张卡 ｜ 复习队列：到期优先、每 4 张到期卡混 1 张新卡。

## 开发

```bash
go test ./...        # 全部单测（切片、调度、队列、仓库层、API）
make dev             # 后端 dev 模式 + web/ 下 npm run dev（Vite 代理 :8080）
./scripts/smoke.sh   # E2E 冒烟（需服务已启动）
```

环境变量：`FUPAN_ADDR`（默认 :8080）、`FUPAN_DATA_DIR`（默认 ./data）、`FUPAN_MILVUS_ADDR`（默认 localhost:19530）、`FUPAN_VECTOR_DISABLED=1`（禁用向量）。

## 版本路线

- **V0.5（当前）**：多库 / MD·TXT 导入 / 切片 / AI 卡片 / FSRS 复习 / 溯源
- V1.0：答题模式（Agent 判级）· 追问变式 · 知识库写回（HITL）· 遗忘曲线面板
- V1.5：PDF / Anki / Obsidian / Notion 导入 · 难卡箱 · 统计
- V2.0：移动端（Capacitor）

## 许可

MIT
