# 复磐 FuPan

> 把你自己的笔记变成一个会安排复习、能溯源到原文的个人记忆系统。
> 本地优先 · 开源 MIT · 单二进制

**核心闭环**：文档导入 → 按标题树切片 → 向量化入库（仅 NoAuth 可选 Milvus）→ AI 生成问答卡（每张卡可溯源到原文）→ FSRS 遗忘曲线调度复习 → 翻卡自评 → 下次复习时间自动排程。

## 5 分钟快速开始

FuPan 支持两种部署模式：**独立部署默认无登录**，适合个人复刻和直接运行；**联合部署启用 OIDC SSO**，适合接入已有网站账号体系。

### 独立部署（默认，无登录系统）

Docker Compose：

```bash
docker compose -f deployments/docker-compose.yml up -d
open http://localhost:8080
```

或使用单二进制（需要 Go 1.26+ 与 Node 22+）：

```bash
make build
./fupan
open http://localhost:8080
```

该模式使用 `data/fupan.db`，不需要 Casdoor 或其他账号服务。仅个人本机使用时建议 `./fupan` 改为 `FUPAN_ADDR=127.0.0.1:8080 ./fupan`。

> **安全提醒：独立部署没有访问控制。** 只要能访问监听地址，就能读写知识库和设置；不要为了“公开分享”直接暴露到不受信任的网络。

### 联合部署（OIDC SSO）

设置 `FUPAN_NO_AUTH=false` 即进入联合部署模式。先在 Casdoor 或其他兼容 OIDC 的身份服务中创建应用，将回调地址设为 `https://fupan.example.com/auth/callback`，再配置：

```bash
export FUPAN_NO_AUTH=false
export FUPAN_OIDC_ISSUER=https://sso.example.com
export FUPAN_OIDC_CLIENT_ID=your-client-id
export FUPAN_OIDC_CLIENT_SECRET=your-client-secret
export FUPAN_REDIRECT_URL=https://fupan.example.com/auth/callback
export FUPAN_ADMIN_SUB=admin-sub
export FUPAN_ALLOWED_SUBS=user-a-sub,user-b-sub  # 可选；admin 自动加入白名单

docker compose -f deployments/docker-compose.yml up -d
```

OIDC 配置不完整会启动失败，不会降级为无登录。Compose 保留 `8080` 和 `/data` 卷，并设置 `FUPAN_VECTOR_DISABLED=1`。公网部署还需在应用前配置 HTTPS 反向代理，使外部地址与 `FUPAN_REDIRECT_URL` 同源；示例本身不提供 TLS。

### 首次使用

1. 左侧「＋ 新建知识库」建一个库（如「考研库」）；
2. 进入库 → 拖入 `.md` / `.txt` 文档 → 自动切片（Markdown 按标题树、TXT 滑窗，代码块/表格不拆开）；
3. 右上角「⚙️ 设置」配置模型——**对话与嵌入是两个独立配置**，均走 OpenAI 兼容协议，内置服务商预设（DeepSeek / 智谱 / OpenAI / Ollama 本地 / 自定义）一键填充，配置后可点「测试连接」验证：
   - **对话模型**（生成卡片用）：DeepSeek `deepseek-chat`、智谱 `glm-4` 等，填 Base URL + API Key；
   - **嵌入模型**（向量化用）：仅 NoAuth 可选；OIDC 首版强制禁用向量，嵌入测试与语义检索不可用；
4. 回到文档 → 按小节分组勾选 →「生成卡片预览」→ 勾选确认入库；
5. 「▶ 开始复习」→ 空格翻卡 → 按 1–4 评分（1=完全想不起来 2=有点模糊 3=想起来了 4=秒答）；
6. 卡片右下角「📄 来源徽章」可回跳高亮原文；关掉重开，复习进度都在。

## 认证、数据与配置

### 本地开发

```bash
make dev             # 显式设置 NoAuth + 127.0.0.1:8080，启动 Go 后端
npm run dev --prefix web  # 可选：另开终端启动 Vite，代理 /api 与 /auth 到 :8080
./scripts/smoke.sh   # NoAuth 本地冒烟；需服务已启动
```

`make dev` 等价于：

```bash
FUPAN_NO_AUTH=1 FUPAN_ADDR=127.0.0.1:8080 go run ./cmd/fupan -dev
```

Vite 虽保留 `/auth` 代理，但 **Vite + OIDC 不是默认开发流程**；本地开发统一使用上述 NoAuth。OIDC 应在真实 HTTPS 部署后用 Casdoor 双账号验收。

### 部署模式与 OIDC 环境变量

`FUPAN_NO_AUTH=true`（默认值）表示独立部署；`FUPAN_NO_AUTH=false` 表示联合部署并要求以下 OIDC 配置。变量均通过环境注入，`-config` 目前**未实现**，传入会报错：

| 变量 | 说明 |
|---|---|
| `FUPAN_OIDC_ISSUER` | 必填，Casdoor/OIDC issuer 的 HTTPS URL |
| `FUPAN_OIDC_CLIENT_ID` | 必填，OIDC Client ID |
| `FUPAN_OIDC_CLIENT_SECRET` | 必填，OIDC Client Secret |
| `FUPAN_REDIRECT_URL` | 必填，完整 HTTPS 回调 URL，例如 `https://fupan.example.com/auth/callback` |
| `FUPAN_ADMIN_SUB` | 必填，管理员在 issuer 内的稳定 `sub`；自动加入白名单，并用于旧库迁移映射 |
| `FUPAN_ALLOWED_SUBS` | 可选，逗号分隔的其他允许登录用户 `sub`；回调及每次请求都会检查 |
| `FUPAN_ALLOWED_ENDPOINT_HOSTS` | 可选，逗号分隔的额外 LLM/搜索端点 host；在内置允许列表上追加，OIDC 自定义端点必须使用 HTTPS |

通用变量：`FUPAN_ADDR`（默认 `:8080`；个人本机建议 `127.0.0.1:8080`）、`FUPAN_DATA_DIR`（默认 `./data`）、`FUPAN_VECTOR_DISABLED=1`（禁用向量）。独立部署还可选 `FUPAN_MILVUS_ADDR`（默认 `localhost:19530`）。

### 每用户数据与秘密

- OIDC 模式的 `data/control.db` 只保存用户映射、OIDC state 和服务端会话（原始 token 不落库）；每个用户的数据独立存放在 `data/users/<内部UUID>/fupan.db`，知识库、复习进度和设置互不可见。
- 请求只使用当前会话对应的请求级 `TenantServices`，不会在共享 Store 上切换用户。未认证请求不会创建租户目录。
- `FUPAN_LLM_API_KEY` 等环境 LLM Key **仅在 NoAuth 模式作为兜底**。OIDC 用户必须在设置页自行配置 Key，不会继承部署环境秘密。
- OIDC 首版始终使用 Noop 向量存储，即使配置了 Milvus 地址也不会连接；`library_id`/UUID 不是授权边界。公网示例同时以 `FUPAN_VECTOR_DISABLED=1` 明确这一部署约束。
- 前端登出通过 `POST /auth/logout` 删除服务端会话并清除 Cookie，随后进入 `/auth/logged-out` 落地页，可从该页重新登录；它不等同于注销整个 Casdoor SSO 会话。

## 离线旧库迁移与回滚

仅旧部署存在 `data/fupan.db` 且需要归给 admin 时执行。迁移命令不会启动 HTTP，目标库已存在时拒绝覆盖；新旧服务不能同时运行。

1. **保留旧二进制，停服并备份**：停止旧服务，备份整个旧数据目录及部署配置。不要直接复制仍在写入的 SQLite/WAL 文件。
2. **配置 OIDC 环境**：在同一 shell 设置 `FUPAN_NO_AUTH=false`、上表 OIDC 变量和正确的 `FUPAN_DATA_DIR`。`-config` 不能替代环境变量。
3. **执行显式离线迁移**：运行 `./fupan -migrate-legacy`。命令通过 SQLite 一致性备份读取旧根库，在临时副本上升级并执行 integrity、foreign-key 和行数校验，再原子落位到 admin 租户；源根库只读保留，并输出额外备份路径。
4. **启动 OIDC 服务**：确认 HTTPS、白名单及 `FUPAN_VECTOR_DISABLED=1`，再启动新二进制。admin 的旧根库存在且尚未迁移时，服务会要求先迁移，不会自动创建空库代替。
5. **真实双用户验收**：两个白名单账号分别完成 Casdoor 登录，将各自原始 session token 放入环境变量后执行（token 不会由脚本打印）：

   ```bash
   FUPAN_A_COOKIE='user-a-raw-token' FUPAN_B_COOKIE='user-b-raw-token' \
     ./scripts/smoke_multiuser.sh https://fupan.example.com
   ```

   脚本验证 `/api/me`、同名知识库列表、模型设置和 direct document ID 隔离，并清理本次创建的数据。它只用于部署后的真实 OIDC 验收，不能用于伪造本地公网成功。
6. **补充验收**：重启确认数据持久化，验证 POST 登出和落地页，移除白名单后确认旧会话也无法继续访问。
7. **回滚**：若验收失败，停止新服务，保留迁移生成物用于排查，使用事先保留的旧二进制和备份恢复原部署。旧根库在迁移中未被修改，仍应保留，不要在回滚前删除。

## 独立部署可选：DeepSeek 出卡 + Ollama 本地嵌入

对话和嵌入拆开后，可以在本地 NoAuth 模式混搭云端/本地模型——例如卡片生成用 DeepSeek，向量化用本地 Ollama：

```bash
brew install ollama && ollama serve
ollama pull bge-m3

# 设置页：对话模型选 DeepSeek；嵌入模型选 Ollama
# Base URL http://localhost:11434/v1，模型 bge-m3，维度 1024
```

NoAuth 如需语义检索，可自行准备 Milvus 并设置 `FUPAN_MILVUS_ADDR`；未设置 `FUPAN_VECTOR_DISABLED=1` 且 Milvus 不可达时会自动降级。公网 OIDC Compose 示例不包含 Milvus，也不能用上述方式启用 OIDC 向量。

> 嵌入维度必须与模型实际输出一致（bge-m3=1024），否则 Milvus 建集合会失败。切换嵌入模型后需重新导入文档以重建向量。

## 架构

```
web (React+Vite+Tailwind)  ──embed──▶  fupan 单二进制 (Go/Gin)
                                          │
              ┌───────────────────────────┼──────────────────────┐
        SQLite (每用户独立库)        Milvus (仅 NoAuth 可选)      LLM（OpenAI 兼容协议）
        control.db + 用户 DB          OIDC 强制 Noop             对话/嵌入各自独立端点
```

- **双粒度索引**：NoAuth 可选将子块（检索单元）进 Milvus，父块（上下文单元）存 SQLite；
- **切片与嵌入解耦**：`Chunker` 接口（`heading` / `fixed` 两个实现）；
- **请求级租户装配**：OIDC 按内部 UUID 懒加载每用户 SQLite 与服务；
- **存储可替换**：Repository 与 VectorStore 均为接口。

## 硬约束（设计原则）

| 原则 | 落实 |
|---|---|
| 原文不可变 | chunk content 永不 UPDATE，修订一律版本 INSERT |
| 溯源必填 | `cards.source_chunk_id` NOT NULL 外键，提交时显式校验 |
| 切片与嵌入解耦 | Chunker 接口，策略不绑定模型 |
| 存储可替换 | Repository / VectorStore 接口化 |
| LLM 不绑定厂商 | 全部走 OpenAI 兼容协议；OIDC 每用户自配 Key |

## 默认参数

目标记忆保持率 0.9 ｜ 每日新卡上限 20 ｜ 每小节 1–3 张卡 ｜ 复习队列：到期优先、每 4 张到期卡混 1 张新卡。

## 测试

```bash
go test ./...
npm run build --prefix web
```

## 版本路线

- **V0.5**：多库 / MD·TXT 导入 / 切片 / AI 卡片 / FSRS 复习 / 溯源 / 纸质主题界面
- **V0.6（进行中）**：卡片分类与管理 · 自由复习 / 随机抽题 · 遗忘曲线面板
- V1.0：答题模式（Agent 判级）· 追问变式 · 知识库写回（HITL）· 遗忘曲线面板
- V1.5：PDF / Anki / Obsidian / Notion 导入 · 难卡箱 · 统计
- V2.0：移动端（Capacitor）

## 许可

MIT
