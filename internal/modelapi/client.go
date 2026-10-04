// Package modelapi implements bounded requests to an administrator-selected target.
package modelapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Error struct{ Code string }

func (e *Error) Error() string  { return "model service: " + e.Code }
func failure(code string) error { return &Error{Code: code} }
func NormalizeBaseURL(raw string) (string, error) {
	if len(raw) > 2048 || strings.TrimSpace(raw) != raw {
		return "", failure("invalid_target")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || (u.Scheme != "http" && u.Scheme != "https") {
		return "", failure("invalid_target")
	}
	if strings.ContainsAny(u.Path, "\\") || u.Opaque != "" {
		return "", failure("invalid_target")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String(), nil
}
func ValidModelID(id string) bool {
	return len(id) > 0 && len(id) <= 256 && utf8.ValidString(id) && strings.TrimSpace(id) == id && strings.IndexFunc(id, unicode.IsControl) < 0
}

type Model struct {
	ID         string `json:"id"`
	Capability string `json:"capability"`
	Selectable bool   `json:"selectable"`
}
type ModelsResult struct {
	Models  []Model `json:"models"`
	Ignored int     `json:"ignored"`
}
type Client struct{ transport http.RoundTripper }

func NewClient() *Client {
	return &Client{transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 120 * time.Second, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}}
}
func (c *Client) request(ctx context.Context, base, path string, key credentials.Secret, body []byte, timeout time.Duration) ([]byte, error) {
	normalized, err := NormalizeBaseURL(base)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	method := http.MethodGet
	var reader io.Reader
	if body != nil {
		method = http.MethodPost
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, normalized+path, reader)
	if err != nil {
		return nil, failure("invalid_target")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key.Value() != "" {
		req.Header.Set("Authorization", "Bearer "+key.Value())
	}
	client := http.Client{Transport: c.transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, failure("cancelled")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, failure("timeout")
		}
		return nil, failure("unreachable")
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode >= 300 && response.StatusCode < 400:
		return nil, failure("redirect_blocked")
	case response.StatusCode == 401:
		return nil, failure("unauthorized")
	case response.StatusCode == 403:
		return nil, failure("forbidden")
	case response.StatusCode == 404 || response.StatusCode == 405 || response.StatusCode == 501:
		return nil, failure("unsupported")
	case response.StatusCode == 429:
		return nil, failure("rate_limited")
	case response.StatusCode == http.StatusBadRequest && (path == "/completion" || path == "/apply-template" || path == "/tokenize"):
		// Pinned llama.cpp returns {error:{type,message,...}}. Read only a
		// bounded machine-code envelope; never return/log the message or body.
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 8193))
		var payload struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		if readErr == nil && len(data) <= 8192 && json.Unmarshal(data, &payload) == nil {
			switch payload.Error.Type {
			case "exceed_context_size_error":
				return nil, failure("context_exceeded")
			case "invalid_request_error":
				return nil, failure("schema_unsupported")
			}
		}
		return nil, failure("unavailable")
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, failure("unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, failure("cancelled")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, failure("timeout")
		}
		return nil, failure("invalid_response")
	}
	if len(data) > 1<<20 {
		return nil, failure("response_too_large")
	}
	return data, nil
}
func (c *Client) Models(ctx context.Context, base string, key credentials.Secret) (ModelsResult, error) {
	data, err := c.request(ctx, base, "/models", key, nil, 10*time.Second)
	if err != nil {
		return ModelsResult{}, err
	}
	var payload struct {
		Data []struct {
			ID   any    `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Data == nil {
		return ModelsResult{}, failure("invalid_response")
	}
	result := ModelsResult{Models: []Model{}}
	seen := map[string]bool{}
	for _, entry := range payload.Data {
		id, ok := entry.ID.(string)
		if !ok || !ValidModelID(id) {
			result.Ignored++
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(seen) > 1000 {
			return ModelsResult{}, failure("response_too_large")
		}
		capability := "unknown"
		selectable := true
		switch entry.Type {
		case "embedding", "image", "audio", "video":
			capability = entry.Type
			selectable = false
		case "chat", "text":
			capability = "text"
		}
		result.Models = append(result.Models, Model{ID: id, Capability: capability, Selectable: selectable})
	}
	sort.Slice(result.Models, func(i, j int) bool { return result.Models[i].ID < result.Models[j].ID })
	return result, nil
}

// Infer accepts only the caller's fixed/validated text contract. It never follows
// tools, output URLs, or instructions to change the request destination.
func (c *Client) Infer(ctx context.Context, base string, key credentials.Secret, model, text string, schema json.RawMessage) (json.RawMessage, error) {
	if !ValidModelID(model) || len(text) > 16000 || !json.Valid(schema) {
		return nil, failure("invalid_request")
	}
	body, err := json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "system", "content": "Extract only explicitly provided metadata into the requested JSON schema. Do not invent missing values. Return JSON only."}, {"role": "user", "content": text}}, "temperature": 0, "max_tokens": 1024, "response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "metadata", "strict": true, "schema": schema}}})
	if err != nil {
		return nil, failure("invalid_request")
	}
	data, err := c.request(ctx, base, "/chat/completions", key, body, 120*time.Second)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				Refusal   any    `json:"refusal"`
				ToolCalls []any  `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &payload) != nil || len(payload.Choices) != 1 {
		return nil, failure("invalid_response")
	}
	choice := payload.Choices[0]
	if choice.FinishReason != "stop" || choice.Message.Refusal != nil || len(choice.Message.ToolCalls) > 0 || !json.Valid([]byte(choice.Message.Content)) {
		return nil, failure("invalid_response")
	}
	return json.RawMessage(choice.Message.Content), nil
}
