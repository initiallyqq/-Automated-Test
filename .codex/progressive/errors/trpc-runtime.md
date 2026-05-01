# trpc-agent-go Runtime

在以下场景加载本文件：

- 事件流中出现 `Response.Error`
- 测试还按旧 fallback 语义写断言
- 想绕开框架直接裸调工具

## 运行时错误上抬

自定义 `model.Model` 时，如果事件流中出现 `Response.Error`，上层执行器必须把它抬回 Go error。

不要把框架错误文案当成正常 JSON 返回。

## 测试语义

现在不能再按“失败后自动 fallback”写测试。

如果没有给足 LLM 响应桩，流程现在应该失败，而不是自动退回旧逻辑。

## 工具执行

对工具执行，优先复用框架 `tool/function` 调用层。

不要重新绕回本地裸调用，避免主路径和框架路径再次分叉。
