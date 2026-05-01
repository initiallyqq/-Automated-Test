# Verification

在以下场景加载本文件：

- 改了 `go.mod`
- 改了 `trpc-agent-go` 相关 import
- 改了 `internal/infra/agentruntime`
- 改了 `internal/app/orchestrator`
- 需要统一验证当前仓库状态

## Go 缓存

优先使用仓库内 Go 缓存：

```powershell
$env:GOCACHE=(Join-Path (Get-Location) '.cache\go-build')
$env:GOMODCACHE=(Join-Path (Get-Location) '.cache\gomod')
$env:GOTELEMETRY='off'
```

## 推荐验证顺序

```powershell
go mod tidy
go test ./internal/infra/agentruntime ./internal/app/orchestrator
go test ./...
```

## 权限说明

如果沙箱或权限环境导致缓存/网络问题，可以提升权限执行。
