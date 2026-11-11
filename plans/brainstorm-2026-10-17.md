# FuPan 头脑风暴落盘（2026-10-17）

> 来源：脑图手绘（导入 markdown/txt → 卡片管理/分类 → 复习两条线）。
> 本文只做「现状核对 + 待办落盘」，不含详细设计；启动某个条目时再单独出实施计划。

## 一、脑图逐项核对

| # | 脑图条目 | 现状 | 结论 |
|---|---|---|---|
| 1 | 导入 markdown/txt，先按段落切分 | `chunker/`（heading 标题树 + fixed 滑窗 + sentence），`service/import.go` | ✅ 已做 |
| 2 | 解析为知识卡片落库 | `service/cardgen.go` 预览→确认提交，`cards.source_chunk_id` 溯源必填 | ✅ 已做 |
| 3 | 卡片可视化可管理性（卡片列表/编辑/删除） | 后端无任何卡片 CRUD 端点（router.go 仅 cardgen + review），前端无卡片管理页 | ❌ 未做 |
| 4 | 同一库支持分类（手动分类） | `Card` 无 category/tag 字段（domain/models.go），全库无分类概念 | ❌ 未做 |
| 5 | 同一库支持分类（智能分类?） | 同上；可基于 LLM 在出卡/入库时打标 | ❌ 未做 |
| 6 | 翻卡复习 · 遗忘曲线记忆法 | FSRS 调度（scheduler + go-fsrs），1–4 自评 | ✅ 已做 |
| 7 | 显示曲线（遗忘曲线可视化面板） | 有 `GET /review/stats`（到期数/新卡数）+ `DueDayHistogram` 按天到期直方图，但没有记忆保持率/复习历史曲线图 | ⚠️ 部分，缺曲线面板 |
| 8 | 可以重复复习，不要锁死 | `ReviewService.Queue` 只取 due<=now 的到期卡 + 新卡，非到期卡无法进入队列 | ❌ 未做 |
| 9 | 非记忆曲线随机题目模式（可选库+分类） | 无该模式；且依赖 #4 分类才有"按分类抽题" | ❌ 未做 |
| 10 | agent 问答式复习（memory 机制帮复习弱项） | 无 agent 会话能力；弱项数据可来自 ReviewLog 的 rating=1/2 历史 | ❌ 未做 |
| 11 | 对知识提出质疑 · agent 联网搜索 | LLM client 仅 chat/embeddings，无联网/检索增强 | ❌ 未做 |
| 12 | 对知识提出质疑 · 修改/新增卡片（写回） | Chunk 已预留 `AuthorAgent`，但无 agent 写回链路（HITL） | ❌ 未做 |

## 二、待办清单（按依赖排序）

### P0 — 分类与卡片管理（其他多项的前置）
1. **Card 加分类字段**：migrations 新增 `category`（或独立 tags 表，倾向先单字段，YAGNI）；出卡 prompt 同步让 LLM 顺手打标（智能分类的最低成本版）。
2. **卡片管理 API**：`GET /libraries/:libId/cards`（分页/按分类筛选）、`PUT /cards/:id`、`DELETE /cards/:id`（软删 status='archived'，不动 chunk）。
3. **卡片管理页（前端）**：库内卡片列表 + 筛选 + 手动改分类/编辑卡面 + 删除；`make build` 走 embed。

### P1 — 复习体验增强
4. **重复复习不锁死**：Queue 增加 `mode=all`（非到期卡也进队），评分照走 FSRS（提前复习即 normal review，go-fsrs 原生支持）；UI 加"自由复习"入口。
5. **随机题目模式**：`GET /libraries/:libId/quiz?category=x&limit=n`，纯随机抽题不写调度（不碰 Schedule/ReviewLog，或可选"不计分"）；入口可选库+分类。
6. **遗忘曲线面板**：基于 ReviewLog + Schedule 历史画保持率/复习量曲线（前端图表；后端补一个按天聚合端点即可）。

### P2 — Agent 复习线（大块，需先出设计文档）
7. **agent 问答式复习**：会话式提问→判级→写回 ReviewLog；memory 机制 = 检索该卡历史 rating=1/2 的 ReviewLog + 卡片原文，让 agent 侧重弱项追问。
8. **知识质疑（联网搜索）**：agent 工具调用接入搜索（具体服务商待选型），引用来源与卡片原文对照。
9. **卡片写回（HITL）**：agent 提议修改/新增 → 用户确认 → 落库；新增卡的 source 溯源策略需先定（复用原 chunk vs 新建 agent chunk，`AuthorAgent` 已预留）。

## 三、与 README 版本路线的对应

- #7、#10、#12 与 README V1.0（答题模式·追问变式·知识库写回·遗忘曲线面板）重合；
- #1–#5、#8、#9 是 V1.0 路线外新增，建议作为 V0.6「分类与自由复习」主题先行。
