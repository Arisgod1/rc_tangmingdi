# API 通知投递系统设计

> 本仓库同时包含完整设计与可运行 MVP。设计说明见下方章节；实现、运行方式和接口示例见文末「MVP 实现与运行」。

## 1. 作业目标与问题理解

企业内部多个业务系统在关键业务事件发生后，需要通知外部供应商提供的 HTTP(S) API。例如：

- 广告平台引流注册成功后，向广告平台回传注册事件；
- 订阅付款成功后，通知 CRM 更新 Contact 状态；
- 商品购买成功后，通知库存系统扣减库存。

不同供应商的 URL、Header、Body 和响应语义可能不同。业务系统不需要同步等待供应商的业务结果，但需要确保通知请求不会因为一次网络失败或服务重启而轻易丢失。

本系统的核心不是简单的 HTTP 转发，而是提供一个统一的通知接入和可靠投递能力：

1. 以规定好的协议接收内部业务系统提交的通知任务；
2. 在任务持久化成功后返回接收确认；
3. 由后台 Worker 异步调用对应的供应商适配器；
4. 根据外部调用结果进行成功、重试或最终失败处理；
5. 持久化通知状态，使任务可查询、可排查、可重放。

## 2. MVP 设计结论

第一版采用单实例服务，使用 SQLite 保存通知任务，并在同一进程中运行持久化 Worker。

```text
业务系统
    |
    | gRPC SubmitNotification
    v
Notification Service
    |
    | 校验、幂等、持久化
    v
SQLite
    |
    | Worker 拉取任务
    v
Provider Adapter
    |
    | HTTP(S)
    v
外部供应商 API
```

gRPC 用于定义内部接入协议和服务边界；可靠性由持久化、任务状态机、Worker、重试和幂等机制共同提供。第一版不把 gRPC 本身当作消息队列或 exactly-once 机制。

如果需要 HTTP/JSON 客户端，可以在 gRPC 服务上增加 grpc-gateway，将 HTTP `POST` / `GET` 映射到 gRPC RPC；内部核心契约仍然是 protobuf/gRPC。

## 3. 系统边界

### 3.1 系统负责

- 提供统一的 gRPC 通知提交接口；
- 校验通知请求和供应商标识；
- 生成通知 ID，并处理幂等键；
- 持久化待投递任务；
- 选择并调用对应的 Provider Adapter；
- 判断外部请求成功、临时失败和永久失败；
- 对可恢复失败执行有限重试和退避；
- 保存任务状态、尝试次数和最近错误；
- 提供通知状态查询；
- 对最终失败任务保留人工重放的可能性。

### 3.2 系统不负责

- 判断业务事件本身是否正确；
- 处理供应商内部的业务事务；
- 保证供应商永久可用；
- 保证 exactly-once 投递；
- 保证 HTTP 2xx 后供应商内部业务一定执行成功；
- 在 MVP 中实现完整的供应商配置后台；
- 在 MVP 中引入复杂的分布式调度、消息编排或运营平台。

如果业务系统要求“业务数据库事务提交成功后通知绝不能丢失”，还需要业务方使用 Outbox 或事务消息。当前 MVP 只保证已经被本服务持久化接受的任务。

## 4. 接口契约

### 4.1 gRPC API

建议定义两个核心 RPC：

```protobuf
service NotificationService {
  rpc SubmitNotification(SubmitNotificationRequest)
      returns (SubmitNotificationResponse);

  rpc GetNotification(GetNotificationRequest)
      returns (GetNotificationResponse);
}
```

提交请求使用统一事件模型，不让业务方直接感知供应商 HTTP 细节：

```protobuf
message SubmitNotificationRequest {
  string provider = 1;
  string event_type = 2;
  string idempotency_key = 3;
  bytes payload = 4;
}
```

`payload` 在 MVP 中可以使用 JSON 字节；供应商 URL、Header 和 Body 格式由服务端的 Adapter 决定。

### 4.2 接收成功语义

`SubmitNotification` 返回 `ACCEPTED` 的前提是：

1. 请求格式合法；
2. provider 对应的 Adapter 存在；
3. 通知任务已经成功提交到数据库事务。

此时返回：

```text
notification_id = <generated-id>
status = ACCEPTED
```

`ACCEPTED` 不表示外部供应商已经收到，只表示本服务已经可靠接收该任务。

以下情况不返回接收成功：

- 参数非法；
- provider 不存在；
- 幂等键与已有任务内容冲突；
- 数据库不可用或事务提交失败。

### 4.3 HTTP 映射

如果启用 grpc-gateway，可以提供：

```text
POST /v1/notifications
GET  /v1/notifications/{id}
```

HTTP 是外部表现形式，内部仍通过统一的 protobuf 请求进入 service 层。

## 5. 分层与职责

### API / Service

负责解析 protobuf、完成基础参数校验、转换领域对象并调用 Domain。Service 不负责 HTTP 供应商调用，也不负责重试状态机。

### Domain

负责通知系统本身的业务规则：

- 创建通知任务；
- 幂等判断；
- 状态流转；
- 选择重试策略；
- 判断是否进入最终失败；
- 计算下次重试时间。

Domain 不依赖 protobuf、HTTP 状态码或具体供应商 JSON 格式。

### Datasource

负责 SQLite 持久化和读取：

- 创建任务；
- 查询任务；
- 原子领取待投递任务；
- 更新任务状态；
- 保存尝试次数、下次重试时间和错误信息。

### Worker

负责异步执行：

1. 查找到期的待投递任务；
2. 原子领取任务；
3. 调用 Adapter；
4. 将 Adapter 的结果交给 Domain；
5. 持久化新的任务状态。

### Adapter

负责屏蔽供应商之间的协议差异：

- 构造 URL；
- 组装 Header；
- 将统一 payload 转换为供应商 Body；
- 调用 HTTP(S)；
- 解析供应商响应；
- 将结果转换为统一的投递结果。

Adapter 不负责整个通知生命周期，只负责“如何与某个供应商通信”。

## 6. 任务状态与可靠性语义

### 6.1 状态

```text
PENDING       已持久化，等待投递
DELIVERING    已被 Worker 领取，正在投递
RETRY_WAIT    本次失败，等待下一次重试
SUCCEEDED     外部 HTTP 投递成功
DEAD          超过最大重试次数或确认不可重试
```

### 6.2 成功与失败

建议将外部 HTTP 2xx 视为 HTTP 层投递成功。

可重试失败：

- 网络连接失败；
- DNS 失败；
- 请求超时；
- HTTP 408；
- HTTP 429；
- HTTP 5xx。

不可重试失败：

- 请求无法序列化；
- URL 或供应商配置非法；
- HTTP 400、401、403 等明确的请求或权限错误；
- 供应商明确返回不可恢复的业务错误。

请求已经发出但响应丢失时，无法确定供应商是否已经处理。系统按至少一次语义将其视为可重试，因此极端情况下可能产生重复请求。业务方应提供幂等键；如果供应商支持，也将幂等键传递给供应商。

### 6.3 重试策略

MVP 使用有限重试和指数退避：

```text
1s -> 5s -> 30s -> 5m -> 30m
```

最大尝试次数暂定为 5 次，达到上限后进入 `DEAD`。重试次数、最近错误、下次执行时间都持久化在数据库中。

重试由 Domain/Worker 统一负责。Adapter 只将供应商结果归一化为：

```text
Succeeded
Retryable
Permanent
Unknown
```

这样可以避免每个 Adapter 重复实现一套任务状态机。

## 7. 数据模型

MVP 可以使用一张核心表：

```text
notifications
- id
- provider
- event_type
- payload
- idempotency_key
- status
- attempts
- next_attempt_at
- lease_until
- last_error
- created_at
- updated_at
- delivered_at
```

`idempotency_key` 建立唯一约束。相同幂等键重复提交相同内容时返回已有任务；同一幂等键对应不同内容时返回冲突。

`lease_until` 用于防止 Worker 在崩溃后永久占用任务。超过租约时间的 `DELIVERING` 任务可以重新进入投递流程。

## 8. Provider Adapter 设计

建议定义统一接口：

```go
type Adapter interface {
    Deliver(ctx context.Context, notification Notification) DeliveryResult
}
```

推荐目录：

```text
internal/adapter/
├── adapter.go
├── registry.go
├── crm/
│   └── adapter.go
├── inventory/
│   └── adapter.go
└── webhook/
    └── adapter.go
```

供应商是外部协议差异，不需要复制完整的 Domain、Datasource 和 RPC 入口。所有供应商共享通知任务的生命周期和重试框架。

如果某个供应商后续具备独立部署、独立扩缩容或独立团队维护的必要，再考虑把该 Adapter 演进成独立 gRPC 服务。

## 9. 建议目录结构

```text
notification/
├── cmd/
│   └── notification/
│       └── main.go
├── api/
│   └── pb/
│       ├── server.proto
│       ├── server.pb.go
│       └── server_grpc.pb.go
├── internal/
│   ├── service/
│   │   └── grpc.go
│   ├── domain/
│   │   └── notification/
│   │       ├── domain.go
│   │       ├── model.go
│   │       ├── submit.go
│   │       ├── delivery.go
│   │       ├── retry.go
│   │       └── store.go
│   ├── datasource/
│   │   └── notification/
│   │       ├── sqlite.go
│   │       └── repository.go
│   ├── adapter/
│   │   ├── adapter.go
│   │   ├── registry.go
│   │   ├── crm/
│   │   ├── inventory/
│   │   └── webhook/
│   ├── worker/
│   │   └── delivery.go
│   └── registry/
│       └── registry.go
├── migrations/
│   └── 001_create_notifications.sql
├── tests/
│   └── e2e/
├── README.md
└── AI_USAGE.md
```

该结构参考了个人知识库中 Go/gRPC 项目的 `api/pb`、`internal/service`、`internal/domain`、`internal/datasource`、`internal/registry` 分层约定，但不依赖公司内部的 Zeus/Rylai 工具。开源仓库应优先使用标准 `protoc` 或 `buf` 生成 protobuf 代码。

## 10. 取舍与演进

### 为什么选择 SQLite + 单实例 Worker

- 不需要额外部署数据库或消息队列；
- 可以验证任务持久化、服务重启恢复和重试语义；
- 代码和运行方式简单，适合 MVP；
- 便于通过自动化测试复现失败场景。

### 为什么不直接使用 Kafka、Redis Streams 或 Temporal

这些组件可以提供更强的吞吐、调度和编排能力，但会引入额外的部署、运维和学习成本。当前作业关注的是边界、可靠性语义和工程取舍，第一版没有必要先解决大规模分布式问题。

### 后续演进方向

1. SQLite 迁移到 PostgreSQL；
2. 使用多个 Worker，并通过数据库锁或队列实现并发领取；
3. 增加 dead-letter 查询和人工重放接口；
4. 增加 Prometheus 指标、结构化日志和告警；
5. 对不同 provider/event_type 配置不同的重试策略；
6. 当 Adapter 具备独立部署价值时，再拆分为独立 gRPC 服务；
7. 业务方需要事务一致性时，引入 Outbox 或事务消息。

## 11. 测试与验收标准

MVP 至少验证：

- 合法请求可以通过 gRPC 提交并持久化；
- 数据库写入失败时不会返回 `ACCEPTED`；
- 重复幂等键不会产生重复任务；
- Worker 可以从 SQLite 拉取任务并调用 Adapter；
- 外部服务第一次返回 500、后续返回 200 时，任务最终为 `SUCCEEDED`；
- 网络超时和 5xx 会进入 `RETRY_WAIT`；
- 明确的 4xx 不会无限重试；
- 达到最大尝试次数后任务进入 `DEAD`；
- Worker 或进程重启后，未完成任务仍可以继续投递；
- 两个 Worker 不会同时处理同一租约内的任务；
- 可以通过 `GetNotification` 查询任务状态和最近错误。

## 12. AI 使用说明

### AI 提供帮助的地方

- 帮助将作业需求拆解为系统边界、可靠性语义和演进问题；
- 帮助识别“业务方不关心外部响应”和“系统内部必须处理外部响应”之间的区别；
- 协助比较 gRPC、HTTP、持久化队列、Worker 和 Provider Adapter 的职责；
- 协助设计至少一次投递、幂等键、失败分类、指数退避和最终失败状态；
- 协助将个人知识库中的 service/domain/datasource/registry 分层经验迁移到新的 MVP；
- 协助评估 SQLite、消息队列和独立 gRPC 服务之间的复杂度取舍；
- 协助整理目录结构、测试场景和 README 内容。

### AI 建议但没有直接采纳的内容

- 没有在 MVP 中引入 Kafka、Temporal、Redis Streams 等重量级基础设施；
- 没有把每个供应商 Adapter 一开始就拆成独立微服务；
- 没有使用“任意 URL + 任意 Header + 任意 Body”的完全通用代理协议，以避免 SSRF 和安全边界不清；
- 没有承诺 exactly-once，而是选择更现实的至少一次投递；
- 没有把重试逻辑分别复制到每个 Adapter，而是集中由 Domain/Worker 管理；
- 没有强依赖个人实习环境中的 Zeus/Rylai 工具，而是保留目录思想并使用标准开源 gRPC 工具链。

### 由本人做出的关键决策

- 选择 gRPC 作为内部通知接入协议，因为个人已有 Go/protobuf/gRPC 的实践经验，并且需要稳定、强类型的服务契约；
- 选择单实例 SQLite + 持久化 Worker 作为 MVP，以降低基础设施复杂度并验证核心可靠性语义；
- 将供应商差异限制在 Adapter 中，不让供应商细节扩散到 Domain 和 Service；
- 采用持久化后的至少一次投递，而不是声称无法可靠保证的 exactly-once；
- 规定只有任务持久化成功后才返回 `ACCEPTED`；
- 认为重试、状态记录和最终失败属于本系统边界，因为它们直接决定通知是否可靠；
- 第一版保持 Adapter 进程内实现，只有在独立部署、扩缩容或团队边界真正出现时再拆成 gRPC 服务。

## 13. 交互记录

### 13.1 方案的提出过程

最初从作业要求出发，确认系统需要接收内部业务系统提交的外部 HTTP 通知，并尽可能可靠地投递到供应商 API。作业明确要求说明系统边界、失败策略、投递语义、复杂度取舍和未来演进，而不是单纯完成一个 HTTP 请求转发器。

随后结合个人在 `rules_dairy` 中沉淀的 Go/gRPC 经验，提出使用 gRPC 作为通知服务的内部接入协议。最初的思路是：业务系统通过规定好的接口提交通知，服务接收后根据不同业务选择对应适配器，再由适配器调用外部 HTTP。

在进一步讨论后，方案逐渐明确为：

```text
统一 gRPC 接入
    -> Domain 处理通知业务规则
    -> Datasource 持久化任务
    -> Worker 异步调度
    -> Adapter 适配供应商 HTTP
```

最后收敛为单实例 SQLite + 持久化 Worker 的 MVP，而不是直接引入消息队列或拆分多个微服务。

### 13.2 交互中的思维碰撞

#### gRPC 是否等于可靠性

最初倾向于把 gRPC 作为输入和输出适配层，并认为使用 gRPC 可以提升可靠投递能力。讨论后明确：gRPC 主要解决内部调用契约、类型安全和服务边界问题，不能替代持久化、队列、重试和幂等。

#### 业务方是否关心外部接口结果

业务方不需要同步等待供应商响应，但系统内部必须读取并持久化外部结果。否则无法判断任务是否成功，也无法实现重试和最终失败处理。

#### 每个供应商是否都应该是一套完整子服务

曾考虑按供应商分别组织 domain、datasource、RPC 入口和 adapter。讨论后认为供应商只是外部协议差异，不应该复制通知状态机和重试逻辑。最终决定共用一个通知 Domain，使用多个 Provider Adapter。

#### 是否一开始拆分独立 gRPC Adapter 服务

考虑到未来不同供应商可能需要独立部署和维护，保留了 Adapter 演进为独立 gRPC 服务的方向。但 MVP 阶段不提前拆分，以避免引入额外网络失败、服务发现、版本管理和多层重试问题。

#### 如何定义成功

讨论将成功拆成两个层次：

1. `ACCEPTED`：通知任务已持久化，本服务可靠接收；
2. `SUCCEEDED`：外部供应商 HTTP 层返回成功。

这样既满足业务方异步提交，也避免把接收成功误认为外部投递成功。

#### 重试是否属于系统边界

最终判断是：重试属于系统边界。题目目标是尽可能可靠地送达目标地址，只保存任务而不继续投递无法满足目标。Domain/Worker 负责重试状态机，Adapter 负责将供应商响应归一化。

#### 业务事务与通知事务是否一致

进一步识别出：gRPC 调用成功不等于业务数据库事务和通知任务处于同一个事务。如果未来要求业务成功后通知绝不丢失，需要业务方 Outbox 或事务消息。MVP 先明确这一点属于系统边界之外。

## 14. 当前实现计划

1. 创建 Go module 和标准 protobuf/gRPC 生成流程；
2. 编写 `SubmitNotification` 和 `GetNotification` proto；
3. 实现 SQLite migration 和通知 Repository；
4. 实现 Notification Domain、状态机和幂等键；
5. 实现一个通用 Webhook Adapter 和一个示例供应商 Adapter；
6. 实现单进程持久化 Worker；
7. 添加重试、退避和 `DEAD` 状态；
8. 添加 gRPC、Domain、Adapter 和重启恢复测试；
9. 补充 README、AI 使用说明和运行命令。

## 15. MVP 实现与运行

### 技术栈与结构

实现采用 Go + gRPC + grpc-gateway v2 + SQLite（`modernc.org/sqlite`，无 CGO 依赖），单进程内同时运行 gRPC/HTTP 服务与持久化 Worker：

```text
cmd/notification/        服务入口（配置、优雅关闭、双端口）
api/proto/               通知协议与 HTTP 映射
api/pb/                  已提交的 protobuf 生成代码
internal/service/        gRPC 服务与 HTTP gateway 解码
internal/domain/         通知状态机、幂等与重试规则
internal/datasource/     SQLite Repository 与 migration
internal/adapter/        Webhook / Mock Provider Adapter
internal/worker/         领取、投递、结果落库与租约恢复
tests/e2e/               gRPC + HTTP 端到端测试
```

### 依赖与代码生成

```bash
brew install protobuf
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.8
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v2.27.3
make generate
```

生成代码已提交，评审和运行测试不依赖本地再次生成。`third_party/googleapis` 中的 `google/api/*.proto` 是 grpc-gateway 注解的官方声明，随仓库提交以保证生成可复现。

### 运行

```bash
go test ./...        # 全部测试
go test -race ./...  # 并发与数据竞争检查
go run ./cmd/notification
```

默认监听 `:9000`（gRPC）与 `:8080`（HTTP gateway），数据落在 `notification.db`。环境变量：

```bash
GRPC_ADDR=:9000                # gRPC 监听地址
HTTP_ADDR=:8080                # HTTP JSON 监听地址
DB_PATH=notification.db        # SQLite 文件
WORKER_POLL_INTERVAL=1s        # Worker 轮询间隔
WORKER_LEASE_DURATION=30s      # 领取租约时长
WEBHOOK_URL=                   # 设置后启用 webhook provider
WEBHOOK_HEADERS_JSON='{"X-API-Key":"secret"}'
WEBHOOK_TIMEOUT=10s
```

不设置 `WEBHOOK_URL` 时默认只启用 `mock` provider，用于本地演示；`webhook` provider 的目标 URL 只来自服务端配置，请求方不能指定任意 URL。

### 接口示例

提交通知（HTTP JSON；`payload` 接受原始 JSON 或 base64）：

```bash
curl -X POST http://localhost:8080/v1/notifications \
  -H 'Content-Type: application/json' \
  -d '{"provider":"mock","eventType":"user.registered","idempotencyKey":"order-42","payload":"{\"user_id\":42}"}'
```

查询状态：

```bash
curl http://localhost:8080/v1/notifications/{notification_id}
```

### 实现要点

- 只有 SQLite 事务提交成功才返回 `ACCEPTED`；重复幂等键且内容一致返回原任务，内容不同返回冲突。
- Worker 用 `lease_until` 原子领取任务，同一租约内并发 Worker 不会重复处理；启动时恢复过期 `DELIVERING` 任务。
- HTTP 2xx 视为投递成功；网络/超时/408/429/5xx 进入 `RETRY_WAIT`；4xx 等明确错误进入 `DEAD`。
- 重试退避 `1s -> 5s -> 30s -> 5m -> 30m`，最多 5 次尝试，之后进入 `DEAD`，状态、尝试次数与最近错误可查询。
- 投递语义为至少一次；请求发出但响应丢失时按可重试处理，极端情况下可能重复请求，业务方应提供幂等键。
- 不使用 Docker：MVP 只有一个 Go 进程和一个 SQLite 文件，`go run` 即可完整验证，引入容器只会增加本地的构建、镜像和编排开销，资源利用率不高。
- 当前不实现鉴权、供应商配置后台、人工重放 RPC、Prometheus 指标与多 Worker；这些作为后续演进项（见第 10 节）。

### 测试覆盖

Repository、Domain、Webhook Adapter、Worker（重试/永久失败/租约恢复）、gRPC + HTTP gateway 端到端均已覆盖，包括并发领取只允许一个 Worker、幂等冲突、数据库读写往返与 `go test -race`。
