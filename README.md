# 自动化测试系统

Go + `trpc-agent-go` + Playwright 自动化测试系统原型。

## 当前状态与可用性边界

项目已度过骨架阶段。MVP 工作流可以扫描目标应用代码仓库、通过 Agent 运行时规划测试场景、生成隔离的 Playwright 冒烟测试用例、执行测试、诊断测试失败、生成测试补丁、通过 Review Guard 审查、应用补丁并重新运行测试。

当前系统适合自动生成第一批 L0 / smoke E2E，用来发现页面白屏、路由错误、接口 5xx、基础 CRUD 断裂、生成用例不稳定等问题。它还不能承诺“任意项目即插即测并发现深层业务 bug”。深层业务缺陷通常需要目标项目提供明确的测试环境、登录方式、测试账号、种子数据、接口契约和业务断言规则。

因此本项目的演进目标是：先把目标项目的环境配置和契约输入标准化，再逐步把弱断言（如 `status < 500`）升级为业务断言和主动 bug 探测。

`test-app/` 只是一个可本地启动的样例目标应用，用于验证端到端链路。系统核心能力不依赖该目录；真实使用时应通过 `repoPath`、`baseUrl`、目标应用配置文件和目标应用自己的测试数据库/测试环境来运行。

支持两种运行模式：
- **线性模式**（默认）：`Orchestrator.Run()` 逐步执行流水线
- **Graph 模式**：基于 `trpc-agent-go/graph` 的 DAG 工作流，支持条件分支和重试循环

### 正常流程

```text
PROJECT_ANALYSIS → SCENARIO_PLANNING → TEST_GENERATION → TEST_EXECUTION → DONE
```

### 失败修复流程

```text
TEST_EXECUTION (失败)
  → FAILURE_DIAGNOSIS
  → TEST_FIXING
  → REVIEW_GUARD
  → APPLY_PATCH
  → RE_RUN
  → DONE / SUCCEEDED / PARTIAL_SUCCEEDED
  (Graph 模式支持 RE_RUN → FAILURE_DIAGNOSIS 重试循环)
```

如果补丁应用后的重跑通过，任务最终状态为 `SUCCEEDED`；如果仍有失败但诊断和归档完成，任务状态为 `PARTIAL_SUCCEEDED`。

## 已实现功能

- CLI 入口：`cmd/autotest`
- HTTP API 入口：`cmd/server`
- SQLite 元数据存储与嵌入式迁移
- 工作流状态持久化与事件记录
- Agent 运行审计记录
- JSON 产物文件：`artifacts/agent-runs/<taskId>/`
- `trpc-agent-go` 执行器适配
- Qwen JSON 模型适配
- 注册的运行时工具：
  - `repo.scan` — 仓库结构扫描
  - `api.scan` — API 路由扫描
  - `db.scan` — 数据库 Schema 扫描
- Agent 能力：
  - `scenario.plan` — 测试场景规划
  - `failure.diagnosis` — 失败诊断（支持多文件）
  - `test.fix` — 测试修复补丁生成
- Playwright 测试用例生成
- 基于扫描到的 API 与数据模型生成应用级写入验证：通过目标应用接口写入其隔离测试数据库，再通过目标应用读取接口回查
- Node.js Playwright 本地执行器（支持 headed 模式）
- Review Guard 安全审查
- 统一差分补丁应用
- 多文件诊断与修复
- 执行产物汇总（Trace、截图、Playwright 报告）
- 统一 Trace/Audit 结构（`TraceRecorder`）
- Graph 工作流引擎（基于 `trpc-agent-go/graph`）
- **SSE 实时进度流**（`--watch` / API stream）
- **工作流图可视化**（DOT/SVG 导出）

## 命令

最短用法：在目标项目或当前仓库配置好 `.autotest.yaml` 后，直接运行：

```bash
go run ./cmd/autotest
```

编译成本地二进制后更短：

```bash
go build -o autotest.exe ./cmd/autotest
./autotest
```

也可以用 `.env.local` 固定常用参数：

```bash
AUTOTEST_REPO_PATH=test-app
AUTOTEST_WATCH=true
AUTOTEST_GRAPH=false
AUTOTEST_HEADED=false
```

支持的常用环境变量：

```text
AUTOTEST_PROJECT_ID
AUTOTEST_REPO_PATH
AUTOTEST_BASE_URL
AUTOTEST_WATCH
AUTOTEST_GRAPH
AUTOTEST_HEADED
AUTOTEST_VISUAL
AUTOTEST_SLOW_MO_MS
AUTOTEST_FORCE_FAIL
```

命令行参数仍然可用，并且优先级高于环境变量。

```bash
go test ./...
go run ./cmd/autotest tools
go run ./cmd/autotest
go run ./cmd/autotest run
go run ./cmd/autotest run --repo-path . --base-url http://127.0.0.1:3000
go run ./cmd/autotest run --watch true        # 实时进度流
go run ./cmd/autotest run --headed true       # 浏览器可见模式
go run ./cmd/autotest run --graph true        # Graph 工作流模式
go run ./cmd/autotest run --force-fail true   # 强制失败测试修复流程
go run ./cmd/autotest status --task <taskId>
go run ./cmd/autotest report --task <taskId>
go run ./cmd/autotest visualize               # 生成工作流图 (DOT)
go run ./cmd/autotest visualize --format svg --output workflow.svg
go run ./cmd/autotest trace --task <taskId>   # 打开 Playwright Trace Viewer
go run ./cmd/autotest serve
go run ./cmd/server
```

在 Windows 沙盒环境中，建议使用项目本地 Go 缓存：

```bash
export GOCACHE="$PWD/.gocache"
export GOTELEMETRY=off
go test ./...
```

## 目标项目配置

真实项目建议在目标仓库根目录提供 `.autotest.yaml` 或 `.autotest.yml`。CLI 会在 `--repo-path` 指向的目录中自动读取该文件；命令行传入的 `--base-url` 优先级高于配置文件。

最小示例：

```yaml
base_url: http://127.0.0.1:3000

commands:
  start: npm run dev
  seed: npm run test:seed
  reset: npm run test:reset

auth:
  login_url: /login
  username: demo@example.com
  password_env: AUTOTEST_DEMO_PASSWORD

safety:
  blocked_endpoints:
    - DELETE /api/users/*
    - POST /api/payments/*

assertions:
  rules:
    - created notes must be visible in the notes list
    - unauthenticated users must receive 401 or be redirected to login
```

当前已支持读取 `base_url` 并把它用于生成的 Playwright 测试。每次执行前会按 `commands.reset`、`commands.start`、`commands.seed` 的顺序准备目标应用：先重置测试数据，再启动目标应用并等待 `base_url` 可访问，最后执行种子数据命令。流程结束后会停止由系统启动的进程；修复后的重跑也会重新执行 reset/seed。认证、安全黑名单和业务断言字段已作为稳定 schema 落地，后续会继续接入工作流。

## 目标应用与数据库语义

自动化系统自身使用 `.autotest/autotest.db` 保存任务、事件、Agent 审计和执行记录。这是系统元数据库。

被测应用的数据库是另一层概念。测试中说的“入表”指的是通过被测应用真实 API 或 UI 让业务数据写入目标应用数据库。用户应提前将目标应用的测试数据库和生产数据库隔离；系统会把它当作真实业务写入来验证，而不是把测试结果写入系统元数据库。

当前生成器会从 `api.scan` 和 `db.scan` 的结果中推导可执行测试：

- 有 `POST` 接口和数据模型时，生成应用级写入验证。
- payload 从字段名派生，跳过 `id`、`created_at`、`updated_at` 等常见服务端字段。
- 写入成功后，优先通过同集合的 `GET` 接口回查刚写入的数据。
- 如果没有可用 `baseUrl`，生成的测试会降级为确定性的本地 fixture，不访问目标应用。

## 清理与重新运行

如需完全重新跑一遍系统，可以删除运行产物和缓存：

```bash
rm -rf .autotest artifacts .gocache
rm -f autotest.exe test-app/*.db
```

这些文件会在后续初始化、构建或运行时重新生成。不要删除 `runner/playwright/package.json`、`go.mod`、`go.sum`、`cmd/`、`internal/` 等源码和依赖描述文件。

## Qwen 配置

```bash
export QWEN_API_KEY='your-api-key'
export QWEN_BASE_URL='https://dashscope.aliyuncs.com/compatible-mode/v1'
```

`QWEN_BASE_URL` 在使用默认 DashScope OpenAI 兼容端点时可省略。

## Playwright 执行器

执行真实 Playwright 测试前需安装依赖：

```bash
cd runner/playwright
npm install
cd ../..
go run ./cmd/autotest run
```

如果未安装 Playwright 依赖，执行器会自动降级为占位模式，仅验证生成的测试用例文件是否存在。

## API

```bash
# 健康检查
curl http://127.0.0.1:8080/api/v1/health

# 列出工具
curl http://127.0.0.1:8080/api/v1/tools

# 执行任务
curl -X POST http://127.0.0.1:8080/api/v1/tasks/run \
  -H "Content-Type: application/json" \
  -d '{"projectId":"local","repoPath":".","useGraph":true,"headed":true}'

# 查看任务状态
curl http://127.0.0.1:8080/api/v1/tasks/<taskId>

# 查看任务报告（含产物摘要）
curl http://127.0.0.1:8080/api/v1/tasks/<taskId>/report

# 实时进度流 (SSE)
curl -N http://127.0.0.1:8080/api/v1/tasks/<taskId>/stream

# 工作流图
curl http://127.0.0.1:8080/api/v1/workflow/graph           # DOT 格式
curl http://127.0.0.1:8080/api/v1/workflow/graph?format=svg  # SVG 图片
```

## 架构

```
cmd/
  autotest/    CLI 入口
  server/      HTTP API 入口
internal/
  api/              HTTP 处理器与 SSE 流
  app/orchestrator/ 核心流水线与 GraphRunner
  config/           配置加载
  domain/workflow/  领域模型与状态机
  infra/
    agentruntime/   工具注册表 + TraceRecorder + trpc-agent-go 适配
    llm/            Qwen LLM 客户端
    sqlite/         SQLite 持久化与迁移
  tools/
    apiscan/        API 路由扫描器
    dbscan/         数据库 Schema 扫描器
    generator/      Playwright 用例生成器
    guard/          Review Guard 安全策略
    patch/           统一差分补丁应用
    playwright/     Node.js Playwright 执行器
    repo/           仓库结构扫描器
runner/playwright/  Node.js Playwright 执行引擎
e2e/specs/          生成的冒烟测试用例
```

## 后续工作

1. 目标项目配置：继续接入认证信息、安全黑名单和业务断言声明；当前已支持 `base_url`、`commands.start`、`commands.reset` 和 `commands.seed`。
2. OpenAPI / Swagger 优先：当目标项目提供接口契约时，用契约生成更准确的 payload、状态码断言和响应 schema 校验。
3. 认证与数据初始化：支持测试账号登录、storage state 复用、seed/reset 钩子，降低真实项目接入成本。
4. 强断言升级：把 `status < 500` 逐步升级为创建后可查询、字段一致、非法输入返回 400、未授权返回 401/403 等业务断言。
5. Bug-seeking 模式：增加空值、超长、非法枚举、越权访问、重复提交、删除后读取、分页边界等主动探测。
6. 失败诊断与修复：继续优化多文件失败诊断、模板级补丁生成和 Review Guard 策略。
7. 工作流可靠性：完善 Graph checkpoint、中断恢复和长任务续跑。
