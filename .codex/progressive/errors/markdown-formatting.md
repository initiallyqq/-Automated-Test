# Markdown Formatting

在以下场景加载本文件：

- `illegal character U+0023 '#'`
- 对 `.md` 文件跑了 `gofmt`

## 规则

不要对 Markdown 文档运行 `gofmt`。

错误示例：

```powershell
gofmt -w README.md docs\实现进度.md
```

这会直接报：

```text
illegal character U+0023 '#'
```

## 正确做法

`gofmt` 只用于 `.go` 文件。

Markdown 只做普通文本编辑，不做 `gofmt`。
