# SQLite Sandbox

在以下场景加载本文件：

- `disk I/O error`
- `database is locked`
- CLI 写 SQLite 失败

## 背景

桌面沙箱可能阻止 `.autotest/autotest.db` 写入。

## 典型报错

```text
disk I/O error
database is locked
```

## 处理方式

涉及 SQLite 写入的 CLI 验证，必要时用提升权限运行：

```powershell
go run ./cmd/autotest init
go run ./cmd/autotest run
go run ./cmd/autotest report --task <taskId>
```

## 注意

不要并发对同一个 SQLite 数据库执行 `init` / `run`。
