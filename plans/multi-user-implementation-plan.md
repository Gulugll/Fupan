# FuPan 多用户改造实施计划（已确认）

> 选型：方案 B（每用户 SQLite）+ 独立 control.db + 服务端随机会话 + 请求级 TenantServices。
> 约束：少于 10 个白名单用户；不做 LRU；公网首版强制禁用向量；NoAuth 固定 local 并沿用 data/fupan.db。
> 流程：每个阶段开始前重读本文件；严格 RED → GREEN → REFACTOR；每阶段运行 go test ./... 与 web 全量构建，最后运行 go test -race ./...。

## 阶段 0：存储与降级原语

- `internal/repo/sqlite/db.go`：每个 migration 文件和 schema_migrations 记录在同一事务；失败可重试。
- `internal/repo/sqlite/document.go`、`internal/service/import.go`：删除不存在文档返回 ErrNotFound，不调用 VectorStore。
- `internal/service/cardgen.go`、`agentchat.go`、`proposal.go`：校验 chunk→document→library、card→library、proposal→library 关系。
- `internal/vector/vector.go`、`internal/service/import.go`、`agentchat.go`、`internal/server/handlers_settings.go`：向量禁用时所有业务 Embed 路径短路；显式返回功能禁用。
- 测试先行：迁移失败回滚、跨库关系拒绝、删除不存在文档零向量调用、Noop 零 Embed。

## 阶段 1：控制库与认证

- `internal/config/config.go`：OIDC、白名单、NoAuth 配置及启动校验；认证失败不得降级。
- 新增 `internal/auth`：control.db users/sessions/oidc_states，(issuer,sub) 唯一，内部 UUID；随机会话只存 token hash。
- Authorization Code + PKCE；验证 state/nonce/issuer/audience/expiry；一次性消费 state；登录轮换会话。
- Cookie：HttpOnly、Secure、SameSite=Lax、Path=/；POST 登出；写请求 Origin 校验。
- 白名单在回调及每次请求检查；health 和 auth 路由为明确例外。
- 测试先行：OIDC 参数校验、state 重放、会话篡改/过期/登出、白名单撤销、CSRF、控制库故障不回退。

## 阶段 2：请求级租户装配

- 新增 `internal/tenant/manager.go`、`services.go`：内部 UUID 安全路径、懒加载、并发初始化去重、失败不缓存、统一 Close。
- 重构 `internal/server/app.go`、`router.go` 和所有业务 handlers：只能使用请求中的 TenantServices；禁止共享 Store 动态切换。
- API 未登录 401；页面跳登录；未认证请求不得创建租户目录；缺 tenant 必须失败关闭。
- NoAuth 使用 local 和旧 data/fupan.db；OIDC 模式不自动打开旧库。
- 测试先行：双用户全路由隔离、同 UID 并发初始化、路径安全、失败重试、未登录零目录、NoAuth 回归。

## 阶段 3：设置加固与前端接入

- `internal/service/settings.go`、`internal/server/handlers_settings.go`：设置 key allowlist；OIDC 租户不继承环境秘密；限制自定义 Base URL，防 SSRF。
- `web/src/api/client.ts`：统一处理 401；Layout/Settings 显示当前用户和 POST 登出；向量禁用 UI 明确降级。
- `web/vite.config.ts`：开发环境代理 /auth。
- 测试先行：双用户设置/搜索用量隔离、环境 Key 不进入 OIDC 租户、非法 key/URL 拒绝、跨站写请求拒绝、前端全量构建。

## 阶段 4：显式离线旧库迁移

- 新增 legacy migration 命令：停服备份、admin 映射、复制到临时文件、迁移、integrity/foreign-key/行数校验、原子落位。
- 源库只读保留；目标存在拒绝覆盖；admin 未迁移前不得自动建空库。
- 测试先行：WAL 已提交数据、旧版本库升级、重复执行、目标存在、失败回滚、源库不变。

## 阶段 5：公网发布准备与全量验收

- 更新部署配置和 smoke：备份→停服→离线迁移→OIDC/HTTPS/白名单→向量禁用→启动→双用户验证。
- 验证重启持久化、登出、白名单撤销、回滚路径；保留旧二进制和旧根库。
- 全量：go test -race ./...、make build、现有 smoke、新增双用户 smoke；确认 embed bundle hash 更新。

## 阶段 6：向量功能安全门禁（本次只保留禁用与测试）

- 公网 OIDC 模式不得连接 Milvus；不得因配置误开。
- 将来启用前另立设计：强制 tenant scope 或每用户 collection、维度策略、空 Filter 拒绝、表达式安全、补建/删除补偿。
- 本阶段不实现 Milvus 多租户、不实现 LRU、不实现共享协作。

## 阶段 7：空闲租户淘汰与优雅关闭（已确认）

- `internal/tenant/manager.go`：缓存项记录 `lastUsed` 和活动引用；`Acquire` 返回 Services 与幂等 release 闭包，不再暴露无引用的 ServicesFor。
- 每 5 分钟清理空闲 30 分钟且 refs=0 的租户；只关闭连接，不删除 SQLite 文件；部分关闭失败也移除缓存并记录，避免后续 Acquire 复用已关闭 DB。
- janitor 使用 stop/done channel，NewManager 只启动一次，ticker 和 goroutine 均可退出；重复 Close 安全。
- Manager.Close 禁止新 Acquire，等待已有引用全部 Release 后关闭全部资源；release 必须幂等。
- `internal/server/middleware.go`：请求 Acquire，defer release；业务 handlers 不变。
- `internal/server/router.go`：HTTP Server 先 Shutdown 并等待在途请求，再执行 App.Close；先关闭 tenant/local，再关闭 control。
- TDD：活动引用不可淘汰、Release 后过期淘汰、重开数据持久、并发 Acquire/Sweep、release 幂等、Close 等待引用、janitor 退出、HTTP 优雅关闭。
- 全量：go test -race ./...、go test -tags dev ./...、make build、standalone smoke。
