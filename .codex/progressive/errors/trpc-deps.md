# trpc-agent-go 依赖问题

在以下场景加载本文件：

- `missing go.sum entry`
- 模块路径不匹配
- `go.opentelemetry.io/...`
- `go.uber.org/zap`
- `google.golang.org/grpc`

## 正确模块路径

必须使用：

```text
trpc.group/trpc-go/trpc-agent-go
```

不要使用：

```text
github.com/trpc-group/trpc-agent-go
```

否则会出现模块声明路径不匹配。

## 处理方式

新增或切换到 `trpc-agent-go` 相关 import 后，必须执行：

```powershell
go mod tidy
```

否则很容易因为缺少传递依赖的 `go.sum` 记录而导致编译失败。

## 典型症状

- `missing go.sum entry`
- `go.opentelemetry.io/...`
- `go.uber.org/zap`
- `google.golang.org/grpc`

这些通常不是业务代码问题，而是依赖未整理完成。
