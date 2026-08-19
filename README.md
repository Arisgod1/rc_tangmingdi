# API 通知投递系统

企业内部多个业务系统在关键事件发生时，需要可靠地调用外部供应商 HTTP(S) API 进行通知。本仓库实现了该系统的设计文档与可运行 MVP：统一接入、SQLite 持久化、后台 Worker 异步投递、最终失败处理与状态查询。

文档导航：

- [设计文档.md](设计文档.md)：系统设计文档（问题理解、系统边界、接口契约、可靠性语义、数据模型、取舍与演进）。
- [AI使用文档.md](AI使用文档.md)：AI 使用说明（AI 帮助、未采纳建议、人工决策）。

## 技术栈与结构

Go + gRPC + grpc-gateway v2 + SQLite（`modernc.org/sqlite`，无 CGO 依赖），单进程内同时运行 gRPC/HTTP 服务与持久化 Worker：

```text
cmd/notification/        服务入口（配置、优雅关闭、双端口）
api/proto/               通知协议与 HTTP 映射
api/pb/                  已提交的 protobuf 生成代码
internal/service/        gRPC 服务与 HTTP gateway 解码
internal/domain/         通知状态机、幂等与投递结果规则
internal/datasource/     SQLite Repository 与 migration
internal/registry/       统一组装 SQLite 与 Provider Adapter
internal/adapter/        Webhook / Mock Provider Adapter
internal/worker/         领取、投递、结果落库与租约恢复
tests/e2e/               gRPC + HTTP 端到端测试
```

## 重构方向（文档先行）

当前实现按水平分层组织（`domain` / `datasource` / `adapter` / `registry` 各一层）。下一阶段计划按「小业务」模块化重构：每个小业务一个独立文件夹，内部包含 Registry（组装本业务所需功能）、Domain（复杂业务逻辑）、DataSource（每个 DataSource 只对应读写一张表）与 RPC 入口；不同业务提供商放在各自子文件夹，跨业务通过 RPC 协作（例如投递成功后触发多个副作用）。

目标目录结构与取舍见 [设计文档.md](设计文档.md) 第 13 节，本次交互记录见 [AI使用文档.md](AI使用文档.md) 第 4.3 节。

## 依赖与代码生成

```bash
brew install protobuf
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.8
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v2.27.3
make generate
```

生成代码已提交，评审和运行测试不依赖本地再次生成。`third_party/googleapis` 中的 `google/api/*.proto` 是 grpc-gateway 注解的官方声明，随仓库提交以保证生成可复现。

## 运行

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

## 接口示例

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

## 实现要点

- 只有 SQLite 事务提交成功才返回 `ACCEPTED`；幂等键按 `(provider, idempotency_key)` 隔离，重复且内容一致返回原任务，内容不同返回冲突。
- 入口通过 `internal/registry` 统一打开 SQLite 并组装 Provider Adapter，Service 和 Worker 共享同一份 Registry。
- Worker 用 `lease_until` 原子领取任务，同一租约内并发 Worker 不会重复处理；启动时恢复过期 `DELIVERING` 任务。
- HTTP 2xx 视为投递成功；永久失败、临时失败和结果未知均进入 `DEAD`，当前不自动重试，`last_error`、尝试次数与状态可查询。
- 投递失败时 Worker 会记录错误日志，任务进入 `DEAD` 后等待后续人工排查或重放；当前不内置自动重试。
- 投递语义为至少一次；请求发出但响应丢失时结果被归一化为 `Unknown`，业务方仍应提供幂等键。
- 不使用 Docker：MVP 只有一个 Go 进程和一个 SQLite 文件，`go run` 即可完整验证，引入容器只会增加本地的构建、镜像和编排开销，资源利用率不高。
- 当前不实现鉴权、供应商配置后台、人工重放 RPC、Prometheus 指标与多 Worker；演进方向见 [设计文档.md](设计文档.md)。

## 测试覆盖

Repository、Domain、Webhook Adapter、Worker（成功/失败/Unknown/租约恢复）、gRPC + HTTP gateway 端到端均已覆盖，包括并发领取只允许一个 Worker、幂等冲突、数据库读写往返与 `go test -race`。
