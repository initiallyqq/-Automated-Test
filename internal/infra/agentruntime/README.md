# Agent Runtime Adapter

当前目录已经正式接入官方 `trpc-agent-go`，不再保留旧的本地 `LocalAgentExecutor` 路径。

## 当前实现

- `Registry`
  - 注册 `repo.scan`、`api.scan`、`db.scan`
  - 作为项目内工具发现和调用入口
- `JSONExecutorModel`
  - 将现有 `JSONGenerator` 适配为 `trpc-agent-go/model.Model`
  - 当前用于桥接千问 JSON 输出能力
- `TrPCAgentExecutor`
  - 基于 `llmagent` + `runner`
  - 使用 `WithStructuredOutputJSONSchema(...)` 执行结构化 Agent 调用
  - 当前承接：
    - `scenario.plan`
    - `failure.diagnosis`
    - `test.fix`
- `buildTRPCTools(...)`
  - 将本地工具注册表映射为 `trpc-agent-go/tool/function` function tools
- `CallTRPCTool(...)`
  - 使用框架的 callable tool 执行工具

## 当前约束

- Orchestrator 的 Agent 执行主路径已经全部切到 `trpc-agent-go`
- 不再保留旧的本地 Agent fallback
- Project Analysis 当前也通过框架 tool 调用层执行已注册工具

## 下一步

1. 继续把 Project Analysis 组织成统一的 Agent + Tool 编排
2. 在 `trpc-agent-go` 上补 Graph / Cycle 级别流程
3. 让工具调用与 Agent 调用共享统一 trace / audit 结构
