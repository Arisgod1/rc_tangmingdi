# AI 使用说明

## 1. AI 在哪些关键地方提供了帮助

- 把作业需求拆解为系统边界、可靠性语义与演进问题，明确“接收确认”和“投递成功”是两层概念。
- 协助设计 gRPC + grpc-gateway + SQLite 的 MVP 结构，以及 Domain / Datasource / Worker / Adapter 的职责划分。
- 协助梳理任务状态机（PENDING / DELIVERING / RETRY_WAIT / SUCCEEDED / DEAD）、至少一次投递、幂等键、指数退避和租约恢复机制。
- 协助生成 protobuf 契约、实现分层代码，并设计覆盖重试、幂等冲突、并发领取、进程恢复和 HTTP 网关的测试。

## 2. AI 给出过哪些没有被采纳的建议

- 不采用 Kafka、Redis Streams、Temporal 等消息队列或编排框架：MVP 规模下它们增加部署与运维成本，SQLite + 单进程 Worker 足以验证可靠性语义。
- 不采用“请求携带任意 URL + Header + Body”的完全通用代理协议：会让 SSRF 和供应商配置边界失控，目标地址改为服务端静态配置。
- 不把每个供应商实现成独立 Adapter 服务：供应商只是协议差异，共享同一套通知生命周期更简单，独立拆分留到部署与团队边界真正出现时。
- 不承诺 exactly-once：HTTP 投递在响应丢失场景下无法可靠去重，MVP 明确选择至少一次，并要求业务方提供幂等键。
- 不为了单元测试把实现拆成大量细碎抽象：只在 Repository、Adapter 和配置等真实运行边界使用接口。

## 3. 由本人做出的关键决策

- 选择 Go + gRPC + SQLite 作为 MVP 技术栈，兼顾强类型契约、可靠投递实现与本地运行成本。
- 选择单实例持久化 Worker，而不是先引入分布式调度；重试、状态记录和最终失败属于本系统边界。
- 决定只有任务持久化成功后才返回 `ACCEPTED`，避免把接收成功误认为外部投递成功。
- 决定 MVP 不引入 Docker：单个 Go 进程加一个 SQLite 文件即可完整验证，容器化在这里是额外负担而非必要复杂度。
- 决定 HTTP gateway 对 `payload` 同时接受原始 JSON 与 base64，降低手工演示门槛，同时保持 gRPC 协议不变。
