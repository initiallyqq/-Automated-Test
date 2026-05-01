package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type QwenClient struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

func NewQwenClient(model string) (*QwenClient, error) {
	key := os.Getenv("QWEN_API_KEY")
	if key == "" {
		return nil, errors.New("QWEN_API_KEY is not set")
	}
	baseURL := os.Getenv("QWEN_BASE_URL")
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	return NewQwenClientWithOptions(key, model, baseURL, nil)
}

func NewQwenClientWithOptions(apiKey, model, baseURL string, httpClient *http.Client) (*QwenClient, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("api key is required")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("model is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("base url is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &QwenClient{
		APIKey:     apiKey,
		Model:      model,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: httpClient,
	}, nil
}

func (c *QwenClient) GenerateJSON(ctx context.Context, prompt string, schema any) ([]byte, error) {
	if c == nil {
		return nil, errors.New("qwen client is nil")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}
	requestBody, err := c.buildRequest(prompt, schema)
	if err != nil {
		return nil, err
	}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("qwen request failed: status=%d body=%s", httpResp.StatusCode, truncate(string(respBody), 512))
	}

	content, err := parseChatCompletionContent(respBody)
	if err != nil {
		return nil, err
	}
	if !json.Valid(content) {
		return nil, fmt.Errorf("qwen returned non-json content: %s", truncate(string(content), 512))
	}
	return content, nil
}

func (c *QwenClient) buildRequest(prompt string, schema any) (map[string]any, error) {
	userPrompt := prompt
	if schema != nil {
		schemaBytes, err := json.Marshal(schema)
		if err != nil {
			return nil, fmt.Errorf("marshal schema: %w", err)
		}
		userPrompt += "\n\nReturn only JSON that matches this schema or shape:\n" + string(schemaBytes)
	}
	return map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are an automated testing agent. Return only valid JSON with no markdown fences.",
			},
			{
				"role":    "user",
				"content": userPrompt,
			},
		},
		"response_format": map[string]string{"type": "json_object"},
	}, nil
}

func parseChatCompletionContent(body []byte) ([]byte, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode qwen response: %w", err)
	}
	if len(response.Choices) == 0 {
		return nil, errors.New("qwen response has no choices")
	}
	content := strings.TrimSpace(response.Choices[0].Message.Content)
	if content == "" {
		return nil, errors.New("qwen response content is empty")
	}
	return []byte(content), nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
