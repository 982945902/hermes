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

func (p *OpenAICompatible) Test(ctx context.Context, channel model.Channel) error {
	if len(channel.Models) == 0 {
		return fmt.Errorf("channel has no models")
	}
	modelName := channel.UpstreamModel(channel.Models[0])
	body, err := json.Marshal(map[string]any{
		"model":      modelName,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
		"stream":     false,
	})
	if err != nil {
		return err
	}
	req, err := p.BuildChatRequest(ctx, channel, body, modelName)
	if err != nil {
		return err
	}
	resp, err := p.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func rewriteModel(body []byte, upstreamModel string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["model"] = upstreamModel
	return json.Marshal(payload)
}
