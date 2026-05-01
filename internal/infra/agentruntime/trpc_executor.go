package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

var trpcNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

type TrPCAgentExecutor struct {
	model trpcmodel.Model
	tools []trpctool.Tool
}

func NewTRPCAgentExecutor(model trpcmodel.Model, registry *Registry) (*TrPCAgentExecutor, error) {
	if model == nil {
		return nil, errors.New("trpc agent executor model is nil")
	}
	return &TrPCAgentExecutor{
		model: model,
		tools: buildTRPCTools(registry),
	}, nil
}

func NewTRPCAgentExecutorFromJSONExecutor(modelName string, generator JSONExecutor, registry *Registry) (*TrPCAgentExecutor, error) {
	if generator == nil {
		return nil, errors.New("json generator is nil")
	}
	return NewTRPCAgentExecutor(&JSONExecutorModel{
		name:      sanitizeTRPCName(firstNonEmptyTRPC(modelName, "autotest-json-model")),
		generator: generator,
	}, registry)
}

func (e *TrPCAgentExecutor) ExecuteJSON(ctx context.Context, agentName, prompt string, schema any) ([]byte, error) {
	if e == nil || e.model == nil {
		return nil, errors.New("trpc agent executor model is nil")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}

	options := []llmagent.Option{llmagent.WithModel(e.model)}
	if len(e.tools) > 0 {
		options = append(options, llmagent.WithTools(e.tools))
	}
	agentName = sanitizeTRPCName(firstNonEmptyTRPC(agentName, "autotest_agent"))
	ag := llmagent.New(agentName, options...)
	run := runner.NewRunner(
		"autotest-"+agentName,
		ag,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
	)

	runOptions := []trpcagent.RunOption{}
	if schemaMap := normalizeSchemaMap(schema); schemaMap != nil {
		runOptions = append(
			runOptions,
			trpcagent.WithStructuredOutputJSONSchema(
				"runtime_output",
				schemaMap,
				true,
				"Return one JSON object matching the runtime schema.",
			),
		)
	}

	events, err := run.Run(
		ctx,
		"autotest-user",
		"autotest-session-"+agentName,
		trpcmodel.NewUserMessage(prompt),
		runOptions...,
	)
	if err != nil {
		return nil, err
	}

	structured, content, runErr := collectAgentResult(events)
	if runErr != nil {
		return nil, runErr
	}
	if structured != nil {
		return json.Marshal(structured)
	}
	if !json.Valid([]byte(content)) {
		return nil, fmt.Errorf("trpc agent returned non-json content: %s", truncateTRPC(content, 512))
	}
	return []byte(content), nil
}

type JSONExecutorModel struct {
	name      string
	generator JSONExecutor
}

func (m *JSONExecutorModel) GenerateContent(ctx context.Context, req *trpcmodel.Request) (<-chan *trpcmodel.Response, error) {
	if m == nil || m.generator == nil {
		return nil, errors.New("json executor model generator is nil")
	}
	if req == nil {
		return nil, errors.New("model request is nil")
	}

	prompt := buildModelPrompt(req.Messages)
	output, err := m.generator.GenerateJSON(ctx, prompt, structuredOutputSchema(req))
	if err != nil {
		return nil, err
	}

	response := &trpcmodel.Response{
		ID:        fmt.Sprintf("json-executor-%d", time.Now().UnixNano()),
		Object:    trpcmodel.ObjectTypeChatCompletion,
		Created:   time.Now().Unix(),
		Model:     m.Info().Name,
		Done:      true,
		IsPartial: false,
		Choices: []trpcmodel.Choice{{
			Index:   0,
			Message: trpcmodel.NewAssistantMessage(string(output)),
		}},
	}
	ch := make(chan *trpcmodel.Response, 1)
	ch <- response
	close(ch)
	return ch, nil
}

func (m *JSONExecutorModel) Info() trpcmodel.Info {
	name := "autotest-json-model"
	if m != nil && strings.TrimSpace(m.name) != "" {
		name = m.name
	}
	return trpcmodel.Info{Name: name}
}

func buildTRPCTools(registry *Registry) []trpctool.Tool {
	toolMap := buildTRPCToolMap(registry)
	tools := make([]trpctool.Tool, 0, len(toolMap))
	for _, tool := range toolMap {
		tools = append(tools, tool)
	}
	return tools
}

func buildTRPCToolMap(registry *Registry) map[string]trpctool.Tool {
	if registry == nil {
		return nil
	}
	names := registry.List()
	tools := make(map[string]trpctool.Tool, len(names))
	for _, info := range names {
		toolImpl := registry.tools[info.Name]
		if toolImpl == nil {
			continue
		}
		name := sanitizeTRPCName(info.Name)
		description := strings.TrimSpace(info.Description)
		toolCopy := toolImpl
		trpcTool := function.NewFunctionTool(
			func(ctx context.Context, input map[string]any) (map[string]any, error) {
				rawInput, err := json.Marshal(input)
				if err != nil {
					return nil, fmt.Errorf("marshal tool input: %w", err)
				}
				rawOutput, err := toolCopy.Invoke(ctx, rawInput)
				if err != nil {
					return nil, err
				}
				var output map[string]any
				if err := json.Unmarshal(rawOutput, &output); err != nil {
					return nil, fmt.Errorf("decode tool output: %w", err)
				}
				return output, nil
			},
			function.WithName(name),
			function.WithDescription(description),
		)
		tools[info.Name] = trpcTool
	}
	return tools
}

func CallTRPCTool(ctx context.Context, registry *Registry, name string, input any, output any) error {
	if registry == nil {
		return errors.New("tool registry is nil")
	}
	tool := buildTRPCToolMap(registry)[name]
	if tool == nil {
		return fmt.Errorf("tool is not registered: %s", name)
	}
	callable, ok := tool.(trpctool.CallableTool)
	if !ok {
		return fmt.Errorf("tool is not callable: %s", name)
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal tool input: %w", err)
	}
	result, err := callable.Call(ctx, inputBytes)
	if err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	outputBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode tool output: %w", err)
	}
	if err := json.Unmarshal(outputBytes, output); err != nil {
		return fmt.Errorf("decode tool output: %w", err)
	}
	return nil
}

func buildModelPrompt(messages []trpcmodel.Message) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		if text == "" && len(msg.ToolCalls) > 0 {
			toolCalls, err := json.Marshal(msg.ToolCalls)
			if err == nil {
				text = string(toolCalls)
			}
		}
		if text == "" {
			continue
		}
		parts = append(parts, strings.ToUpper(msg.Role.String())+":\n"+text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func structuredOutputSchema(req *trpcmodel.Request) any {
	if req == nil || req.StructuredOutput == nil || req.StructuredOutput.JSONSchema == nil {
		return nil
	}
	return req.StructuredOutput.JSONSchema.Schema
}

func normalizeSchemaMap(schema any) map[string]any {
	if schema == nil {
		return nil
	}
	if direct, ok := schema.(map[string]any); ok {
		return direct
	}
	bytes, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(bytes, &decoded); err != nil {
		return nil
	}
	return decoded
}

func collectAgentResult(events <-chan *event.Event) (any, string, error) {
	var (
		structured any
		content    string
		runErr     error
	)
	for evt := range events {
		if evt == nil {
			continue
		}
		if evt.StructuredOutput != nil {
			structured = evt.StructuredOutput
		}
		if evt.Response == nil {
			continue
		}
		if evt.Response.Error != nil {
			runErr = errors.New(strings.TrimSpace(evt.Response.Error.Message))
		}
		for _, choice := range evt.Response.Choices {
			if strings.TrimSpace(choice.Message.Content) != "" {
				content = choice.Message.Content
			}
			if strings.TrimSpace(choice.Delta.Content) != "" {
				content += choice.Delta.Content
			}
		}
	}
	return structured, strings.TrimSpace(content), runErr
}

func sanitizeTRPCName(value string) string {
	value = trpcNamePattern.ReplaceAllString(strings.TrimSpace(value), "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "autotest_agent"
	}
	return value
}

func firstNonEmptyTRPC(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncateTRPC(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
