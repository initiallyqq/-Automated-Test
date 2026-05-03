# Codex Project Instructions

这是当前仓库的“渐进式披露”入口文件。默认只加载这里；遇到对应错误或场景时，再打开指定子文档。

## 1. 仓库边界

唯一允许改代码的仓库：

```text
D:\mygo\自动化测试系统\-Automated-Test
```

不要在父目录 `D:\mygo\自动化测试系统` 下做实现层代码修改。

## 2. 当前必须知道的事实

- Agent 框架已经是官方 `trpc-agent-go`
- `scenario.plan`、`failure.diagnosis`、`test.fix` 全部走 `trpc-agent-go`
- 旧的本地 `LocalAgentExecutor` 已删除
- 不要默认假设系统还保留 fallback

如果框架执行器或工具入口不可用，应直接暴露错误，而不是偷偷补回旧路径。

## 3. 子文档索引

平时不要一次性加载所有子文档。只在命中对应问题时再读。

### 3.1 通用验证

遇到这些情况时加载：

- 改了 `go.mod`
- 改了框架 import
- 需要跑测试

打开：

[`D:\mygo\自动化测试系统\-Automated-Test\.codex\progressive\verification.md`](D:/mygo/自动化测试系统/-Automated-Test/.codex/progressive/verification.md)

### 3.2 trpc-agent-go 依赖路径 / go.sum 问题

遇到这些报错时加载：

- `missing go.sum entry`
- 模块路径不匹配
- `go.opentelemetry.io/...`
- `go.uber.org/zap`
- `google.golang.org/grpc`

打开：

[`D:\mygo\自动化测试系统\-Automated-Test\.codex\progressive\errors\trpc-deps.md`](D:/mygo/自动化测试系统/-Automated-Test/.codex/progressive/errors/trpc-deps.md)

### 3.3 Markdown / gofmt 误用

遇到这些报错时加载：

- `illegal character U+0023 '#'`
- 对 `.md` 文件跑了 `gofmt`

打开：

[`D:\mygo\自动化测试系统\-Automated-Test\.codex\progressive\errors\markdown-formatting.md`](D:/mygo/自动化测试系统/-Automated-Test/.codex/progressive/errors/markdown-formatting.md)

### 3.4 SQLite 沙箱 / 锁表 / I/O 问题

遇到这些报错时加载：

- `disk I/O error`
- `database is locked`
- CLI 写 SQLite 失败

打开：

[`D:\mygo\自动化测试系统\-Automated-Test\.codex\progressive\errors\sqlite-sandbox.md`](D:/mygo/自动化测试系统/-Automated-Test/.codex/progressive/errors/sqlite-sandbox.md)

### 3.5 trpc-agent-go 运行时行为问题

遇到这些情况时加载：

- `Response.Error` 出现在事件流里
- 测试还按旧 fallback 语义写断言
- 想绕开框架直接裸调工具

打开：

[`D:\mygo\自动化测试系统\-Automated-Test\.codex\progressive\errors\trpc-runtime.md`](D:/mygo/自动化测试系统/-Automated-Test/.codex/progressive/errors/trpc-runtime.md)

## 4. 固定决策

- First project type: fullstack
- LLM: Qwen first
- Agent framework: `trpc-agent-go`
- Test language: TypeScript
- Runner: local Node.js
- Metadata database: SQLite
- Entry points: CLI/API
- Test patches: auto-apply after Review Guard allows them

## 5. Shell preference

- Prefer Git Bash / `bash` for future terminal commands in this repository.
- On this machine, use `D:\Git\bin\bash.exe -lc '<command>'` when invoking shell commands.
- Do not use PowerShell for normal development, verification, or full-chain runs unless Bash is unavailable or a Windows-only command is explicitly required.
- When writing examples or project commands, prefer Bash syntax over PowerShell syntax.
