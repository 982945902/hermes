package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/982945902/hermes/internal/model"
)

type OpenAICompatible struct {
	client *http.Client
}

func NewOpenAICompatible(client *http.Client) *OpenAICompatible {
	return &OpenAICompatible{client: client}
}

func (p *OpenAICompatible) BuildChatRequest(ctx context.Context, channel model.Channel, body []byte, upstreamModel string) (*http.Request, error) {
	rewritten, err := rewriteModel(body, upstreamModel)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(channel.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(rewritten))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+channel.APIKey)
	for key, value := range channel.ExtraHeaders {
		if strings.TrimSpace(key) != "" && value != "" {
			req.Header.Set(key, value)
		}
	}
	if channel.Provider == model.ProviderOpenRouter {
		if req.Header.Get("HTTP-Referer") == "" {
			req.Header.Set("HTTP-Referer", "https://github.com/982945902/hermes")
		}
		if req.Header.Get("X-Title") == "" {
			req.Header.Set("X-Title", "Hermes")
		}
	}
	return req, nil
}

func (p *OpenAICompatible) Do(req *http.Request) (*http.Response, error) {
	return p.client.Do(req)
}

func (p *OpenAICompatible) FetchModels(ctx context.Context, channel model.Channel) ([]string, error) {
	url := strings.TrimRight(channel.BaseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+channel.APIKey)
	for key, value := range channel.ExtraHeaders {
		if strings.TrimSpace(key) != "" && value != "" {
			req.Header.Set(key, value)
		}
	}
	if channel.Provider == model.ProviderOpenRouter {
		if req.Header.Get("HTTP-Referer") == "" {
			req.Header.Set("HTTP-Referer", "https://github.com/982945902/hermes")
		}
		if req.Header.Get("X-Title") == "" {
			req.Header.Set("X-Title", "Hermes")
		}
	}
	resp, err := p.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return parseModelList(data), nil
}

func (p *OpenAICompatible) Test(ctx context.Context, channel model.Channel, modelName string, prompt string) (string, error) {
	if len(channel.Models) == 0 {
		return "", fmt.Errorf("channel has no models")
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = "请用一句话回复：Hermes channel test ok"
	}
	if strings.TrimSpace(modelName) == "" {
		modelName = channel.Models[0]
	}
	upstreamModel := channel.UpstreamModel(modelName)
	body, err := json.Marshal(map[string]any{
		"model":      upstreamModel,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
		"max_tokens": 256,
		"stream":     false,
	})
	if err != nil {
		return "", err
	}
	req, err := p.BuildChatRequest(ctx, channel, body, upstreamModel)
	if err != nil {
		return "", err
	}
	resp, err := p.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return extractAssistantContent(data), nil
}

func extractAssistantContent(data []byte) string {
	var payload struct {
		Choices []struct {
			Message struct {
				Content          any    `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				Refusal          string `json:"refusal"`
			} `json:"message"`
			Text         string `json:"text"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return strings.TrimSpace(string(data))
	}
	if len(payload.Choices) == 0 {
		return strings.TrimSpace(string(data))
	}
	if payload.Choices[0].Text != "" {
		return payload.Choices[0].Text
	}
	if payload.Choices[0].Message.ReasoningContent != "" {
		return payload.Choices[0].Message.ReasoningContent
	}
	if payload.Choices[0].Message.Refusal != "" {
		return payload.Choices[0].Message.Refusal
	}
	switch content := payload.Choices[0].Message.Content.(type) {
	case string:
		if strings.TrimSpace(content) != "" {
			return content
		}
	case []any:
		var b strings.Builder
		for _, item := range content {
			if part, ok := item.(map[string]any); ok {
				if text, _ := part["text"].(string); text != "" {
					b.WriteString(text)
				}
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	raw := strings.TrimSpace(string(data))
	if len(raw) > 4096 {
		raw = raw[:4096] + "...(truncated)"
	}
	if payload.Choices[0].FinishReason != "" {
		return "上游返回了空内容，finish_reason=" + payload.Choices[0].FinishReason + "\n\n原始响应：\n" + raw
	}
	return raw
}

func parseModelList(data []byte) []string {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.ID != "" {
			models = append(models, item.ID)
		}
	}
	return models
}

func rewriteModel(body []byte, upstreamModel string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["model"] = upstreamModel
	return json.Marshal(payload)
}
