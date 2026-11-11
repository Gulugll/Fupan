# P2 Agent 复习线 — Harness 框架设计（2026-10-17）

> 范围：脑图 P2 三项——agent 问答式复习（memory 机制帮弱项）、知识质疑（联网搜索）、修改/新增卡片（HITL 写回）。
> 本文先定 harness（agent 运行框架），三个功能全部长在 harness 之上。

## 一、现成框架调研（2026-10 检索）

| 选项 | 结论 |
|---|---|
| LangChainGo (~9k★) | 不选。pre-1.0、162 个 open PR、依赖重、API 照搬 Python，中看不中用 |
| Eino (ByteDance/CloudWeGo) | 不选。图编排/多 agent/HITL interrupt 很强，但 FuPan 是**单模型+少量工具的直线 agent**，不是图；依赖树大，为用 5% 的能力背整个框架 |
| Genkit Go (Google, 1.0 GA) | 不选。卖点 tracing+dev UI+JS 同构，单二进制本地应用用不上 |
| OpenAI Agents Go（社区移植） | 不选。255★ 绑 OpenAI 语义，与「LLM 不绑定厂商」硬约束冲突 |
| **自写 loop** | **选定**。单模型、<6 个工具、一个 HITL 门——行业共识是这场景 for loop 就是正确代码量（"the honest fourth choice is a for loop"），控制流全自持、零依赖 |

Go 标准库自带并发/取消/超时，框架在 Go 里只剩「图编排」一个卖点，而我们没有图。**结论：自建 ~100 行 harness，铺在现有 `llm.Client`（OpenAI 兼容）上。**

## 二、Harness 架构

```
internal/agent/
  tool.go     # Tool 接口 + 注册表
  runner.go   # agent loop：chat → tool_calls → 分发 → 回填 → 循环直到无 tool_calls 或 maxSteps
internal/llm/client.go  # 扩展：Message 加 tool_calls/tool_call_id，ChatRequest 加 tools
```

```go
// Tool 最小接口（JSON Schema 手写，不引 schema 生成库）。
type Tool interface {
    Name() string
    Description() string
    Parameters() string // JSON Schema 字符串
    Run(ctx context.Context, args json.RawMessage) (string, error) // 结果回填给 LLM
}

// Runner 无状态：调用方传入全部 messages，返回追加后的 messages。
type Runner struct {
    Client   *llm.Client
    Tools    []Tool
    MaxSteps int // 默认 6，防失控
}
func (r *Runner) Run(ctx context.Context, msgs []llm.Message) ([]llm.Message, error)
```

要点：
- **兼容现有代码**：llm 新增 `ChatMessage()`（返回完整 assistant message 含 tool_calls），现有 `Chat()`（返回 string）保留为薄包装，cardgen 不动；
- **无状态会话**：前端持有 messages 全量 POST `/api/agent/chat`，后端跑 loop 返回追加结果。单机无需 session 表、无过期清理、天然可续聊；
- **HITL 铁律**：任何写库动作（改卡/建卡/评分）**工具只产提案，不直接落库**。执行权在用户确认按钮上。

## 三、三个功能如何长在 harness 上

### P2.1 agent 问答式复习（memory 机制 = 复习历史注入）
会话初始 system + 上下文由后端拼好：卡片正背面 + 溯源原文 + **该卡 ReviewLog 历史**（rating 1/2 的次数与时间 = 弱项信号）。
工具：`get_card_detail`、`get_review_history`、`search_library`（复用现有 /search 向量检索，Milvus 降级时该工具不注册）。
流程：agent 提问 → 用户文字回答 → agent 判定掌握度 → 调 `suggest_rating(1-4)` 工具 → **UI 弹出建议分按钮，用户点确认才走现有 POST /cards/:id/review 落库**。agent 只建议，用户裁决。

### P2.2 知识质疑 + 联网搜索
`web_search` 工具（服务商待选型，见问题 2），结果 clip 后回填。agent 被要求：引用来源 URL + 与卡片原文对照，指出矛盾/过时/含糊之处。
`search_library` 与 `web_search` 同时可用时，system prompt 要求先查库内原文再联网对照。

### P2.3 修改/新增卡片（HITL 写回）
新表 `card_proposals`：
```sql
CREATE TABLE IF NOT EXISTS card_proposals (
    id TEXT PRIMARY KEY,
    card_id TEXT,                -- edit 时非空；new 时为空
    kind TEXT NOT NULL,          -- 'edit' | 'new'
    payload TEXT NOT NULL,       -- JSON: {front,back,category} / 同
    status TEXT NOT NULL DEFAULT 'pending',  -- pending | applied | rejected
    rationale TEXT NOT NULL DEFAULT '',      -- agent 给的理由（质疑依据/来源）
    created_at TEXT NOT NULL
);
```
工具 `propose_card_edit` / `propose_card_new` 只写 pending 行 → 会话气泡内嵌提案卡（显示 diff/新卡内容 + agent 理由 + 来源）→ 用户「采纳/拒绝」→ 采纳时 edit 走现有 `UpdateCard`、new 建卡并挂原 chunk（溯源不丢）。

## 四、安全边界
- maxSteps=6，超限返回已获结果并提示；
- 工具按场景白名单注入（复习会话没有直接写库工具，只有 propose）；
- 搜索/检索结果统一 clip（2000 字符）回填；
- 提案执行路径复用现有 service 校验（溯源必填、分类字段），不新开写入口。

## 五、任务拆解（每步 TDD，mock LLM 端点）
1. **P2.1a harness 核心**：llm tools 扩展 + agent.Tool/Runner + 单测（mock server 验证 tool_calls 往返、maxSteps、错误传播）
2. **P2.1b 复习会话**：`/api/agent/chat` 端点 + 复习工具集 + 上下文拼装 + AgentReview 前端页（含建议评分确认）
3. **P2.2 联网搜索**：web_search 工具 + 设置页搜索服务商配置
4. **P2.3 写回 HITL**：card_proposals 表 + propose 工具 + 提案确认 UI

## 六、待用户确认
1. harness 自建（推荐）vs 引 Eino；
2. 联网搜索服务商：Tavily（免费额度、接法简单）/ 博查（国内直连）/ SearXNG 自建 / P2.2 暂缓；
3. 会话状态：stateless 前端持有（推荐）vs 后端会话表。
