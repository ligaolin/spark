// Package ai provides BYOK (bring-your-own-key) chat completions over an
// OpenAI-compatible API. The API key is stored encrypted via the secure
// package; streamed tokens are pushed to the frontend through the "ai:delta"
// event keyed by a request id.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"changeme/app/service/secure"
	"changeme/app/service/settings"
	"changeme/app/service/types"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Setting keys persisted in the settings table. The API key is encrypted
// before storage; the remaining fields are plain-text configuration.
const (
	keyBaseURL      = "ai.baseUrl"
	keyModel        = "ai.model"
	keyTemperature  = "ai.temperature"
	keyMaxTokens    = "ai.maxTokens"
	keySystemPrompt = "ai.systemPrompt"
	keyAPIKey       = "ai.apiKey" // encrypted
)

// AIService exposes BYOK chat completions to the frontend.
type AIService struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// ServiceName implements application.ServiceName.
func (s *AIService) ServiceName() string { return "AIService" }

// GetConfig returns the current AI settings. The API key itself is never
// exposed; HasKey reports whether one is stored.
func (s *AIService) GetConfig() (types.AIConfig, error) {
	cfg := types.AIConfig{
		BaseURL:      settings.GetString(keyBaseURL, "https://api.openai.com/v1"),
		Model:        settings.GetString(keyModel, "gpt-4o-mini"),
		Temperature:  settings.GetFloat(keyTemperature, 0.7),
		MaxTokens:    settings.GetInt(keyMaxTokens, 2048),
		SystemPrompt: settings.GetString(keySystemPrompt, "你是一个乐于助人的助手。"),
	}
	cfg.HasKey = settings.GetString(keyAPIKey, "") != ""
	return cfg, nil
}

// SaveConfig persists AI settings. When apiKey is empty the previously stored
// key is kept; when non-empty it replaces the stored key (encrypted).
func (s *AIService) SaveConfig(cfg types.AIConfig, apiKey string) error {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return errors.New("API 地址不能为空")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return errors.New("模型名称不能为空")
	}
	if err := settings.Set(keyBaseURL, baseURL); err != nil {
		return err
	}
	if err := settings.Set(keyModel, model); err != nil {
		return err
	}
	if err := settings.Set(keyTemperature, strconv.FormatFloat(cfg.Temperature, 'f', -1, 64)); err != nil {
		return err
	}
	if err := settings.Set(keyMaxTokens, strconv.Itoa(cfg.MaxTokens)); err != nil {
		return err
	}
	if err := settings.Set(keySystemPrompt, cfg.SystemPrompt); err != nil {
		return err
	}
	if key := strings.TrimSpace(apiKey); key != "" {
		enc, err := secure.Encrypt(key)
		if err != nil {
			return err
		}
		if err := settings.Set(keyAPIKey, enc); err != nil {
			return err
		}
	}
	return nil
}

// ClearKey removes the stored API key.
func (s *AIService) ClearKey() error {
	return settings.Set(keyAPIKey, "")
}

// ChatStream starts a streaming chat completion. Tokens are delivered to the
// frontend through the "ai:delta" event keyed by requestID. This method
// returns after the request is dispatched, or immediately with an error when
// the configuration is incomplete; stream progress and completion arrive via
// the event.
func (s *AIService) ChatStream(requestID string, messages []types.ChatMessage) error {
	if strings.TrimSpace(requestID) == "" {
		return errors.New("请求 ID 不能为空")
	}
	if len(messages) == 0 {
		return errors.New("消息不能为空")
	}

	cfg, err := s.GetConfig()
	if err != nil {
		return err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("尚未配置 API Key，请在「设置 → AI」中填写")
	}

	// Prepend the system prompt when configured.
	msgs := make([]types.ChatMessage, 0, len(messages)+1)
	if cfg.SystemPrompt != "" {
		msgs = append(msgs, types.ChatMessage{Role: "system", Content: cfg.SystemPrompt})
	}
	msgs = append(msgs, messages...)

	go s.stream(requestID, cfg, apiKey, msgs)
	return nil
}

// Complete runs a one-shot (non-streaming) completion with an explicit system
// prompt and returns the full reply text. Used by the terminal assistant,
// editor actions and the document assistant.
func (s *AIService) Complete(systemPrompt, userContent string) (string, error) {
	if strings.TrimSpace(userContent) == "" {
		return "", errors.New("内容不能为空")
	}
	msgs := []types.ChatMessage{}
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, types.ChatMessage{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, types.ChatMessage{Role: "user", Content: userContent})
	return s.complete(msgs, false)
}

// ChatJSON runs a non-streaming completion in JSON output mode and returns
// the assistant's raw content. Package-level (not bound to the frontend),
// used internally by the terminal agent.
func ChatJSON(messages []types.ChatMessage) (string, error) {
	return (&AIService{}).complete(messages, true)
}

// ChatTools runs a non-streaming completion with the given tool definitions
// and returns the assistant's text content and/or tool calls. Package-level
// (not bound to the frontend); used by the terminal agent for structured
// command execution decisions.
func ChatTools(messages []types.ChatMessage, tools []Tool) (ToolResponse, error) {
	return (&AIService{}).chatTools(messages, tools)
}

func (s *AIService) chatTools(messages []types.ChatMessage, tools []Tool) (ToolResponse, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return ToolResponse{}, err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return ToolResponse{}, err
	}
	if apiKey == "" {
		return ToolResponse{}, errors.New("尚未配置 API Key，请在「设置 → AI」中填写")
	}

	reqBody := struct {
		Model       string              `json:"model"`
		Messages    []types.ChatMessage `json:"messages"`
		Temperature float64             `json:"temperature"`
		MaxTokens   int                 `json:"max_tokens,omitempty"`
		Stream      bool                `json:"stream"`
		Tools       []Tool              `json:"tools,omitempty"`
		ToolChoice  string              `json:"tool_choice,omitempty"`
	}{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: cfg.Temperature,
		Stream:      false,
		Tools:       tools,
		ToolChoice:  "auto",
	}
	if cfg.MaxTokens > 0 {
		reqBody.MaxTokens = cfg.MaxTokens
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return ToolResponse{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ToolResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ToolResponse{}, errors.New("请求超时")
		}
		return ToolResponse{}, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ToolResponse{}, fmt.Errorf("API 返回错误（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ToolResponse{}, fmt.Errorf("解析响应失败: %w", err)
	}
	if len(out.Choices) == 0 {
		return ToolResponse{}, errors.New("模型未返回内容")
	}

	result := ToolResponse{Content: out.Choices[0].Message.Content}
	for _, tc := range out.Choices[0].Message.ToolCalls {
		args := map[string]any{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			args = map[string]any{}
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: args})
	}
	return result, nil
}

// complete performs a non-streaming chat completion and returns the assistant
// content. jsonMode requests JSON output (response_format).
func (s *AIService) complete(messages []types.ChatMessage, jsonMode bool) (string, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return "", err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return "", err
	}
	if apiKey == "" {
		return "", errors.New("尚未配置 API Key，请在「设置 → AI」中填写")
	}

	reqBody := chatRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: cfg.Temperature,
		Stream:      false,
	}
	if cfg.MaxTokens > 0 {
		reqBody.MaxTokens = cfg.MaxTokens
	}
	if jsonMode {
		reqBody.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New("请求超时")
		}
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("API 返回错误（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("模型未返回内容")
	}
	return out.Choices[0].Message.Content, nil
}

// CompleteStream starts a one-shot streaming completion with an explicit
// system prompt. Tokens are delivered through the "ai:delta" event keyed by
// requestID — the same mechanism as ChatStream, but without prepending the
// config's global system prompt.
func (s *AIService) CompleteStream(requestID, systemPrompt, userContent string) error {
	if strings.TrimSpace(requestID) == "" {
		return errors.New("请求 ID 不能为空")
	}
	if strings.TrimSpace(userContent) == "" {
		return errors.New("内容不能为空")
	}

	cfg, err := s.GetConfig()
	if err != nil {
		return err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("尚未配置 API Key，请在「设置 → AI」中填写")
	}

	msgs := []types.ChatMessage{}
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, types.ChatMessage{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, types.ChatMessage{Role: "user", Content: userContent})

	go s.stream(requestID, cfg, apiKey, msgs)
	return nil
}

// Cancel stops an in-flight streaming request by id.
func (s *AIService) Cancel(requestID string) error {
	s.mu.Lock()
	cancel := s.cancels[requestID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (s *AIService) apiKey() (string, error) {
	enc := settings.GetString(keyAPIKey, "")
	if enc == "" {
		return "", nil
	}
	return secure.Decrypt(enc)
}

func (s *AIService) stream(requestID string, cfg types.AIConfig, apiKey string, msgs []types.ChatMessage) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.cancels == nil {
		s.cancels = make(map[string]context.CancelFunc)
	}
	s.cancels[requestID] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.cancels, requestID)
		s.mu.Unlock()
		cancel()
	}()

	if err := s.doStream(ctx, requestID, cfg, apiKey, msgs); err != nil {
		emit(requestID, types.AIChatDelta{RequestID: requestID, Done: true, Error: err.Error()})
		return
	}
	emit(requestID, types.AIChatDelta{RequestID: requestID, Done: true})
}

type chatRequest struct {
	Model          string              `json:"model"`
	Messages       []types.ChatMessage `json:"messages"`
	Temperature    float64             `json:"temperature"`
	MaxTokens      int                 `json:"max_tokens,omitempty"`
	Stream         bool                `json:"stream"`
	ResponseFormat *responseFormat     `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

// Tool describes one function-calling tool definition.
type Tool struct {
	Type     string   `json:"type"` // "function"
	Function ToolFunc `json:"function"`
}

// ToolFunc is the function schema of a tool.
type ToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall is one function call requested by the model.
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolResponse is the assistant message returned by a tool-calling request.
type ToolResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"toolCalls"`
}

func (s *AIService) doStream(ctx context.Context, requestID string, cfg types.AIConfig, apiKey string, msgs []types.ChatMessage) error {
	reqBody := chatRequest{
		Model:       cfg.Model,
		Messages:    msgs,
		Temperature: cfg.Temperature,
		Stream:      true,
	}
	if cfg.MaxTokens > 0 {
		reqBody.MaxTokens = cfg.MaxTokens
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// No overall timeout: a stream stays open until it finishes or the request
	// context is cancelled. Connection setup is bounded by the default
	// transport's dial timeout.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("已停止生成")
		}
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("API 返回错误（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	return parseSSE(ctx, requestID, resp.Body)
}

// parseSSE consumes an OpenAI-compatible chat.completions SSE stream and emits
// one "ai:delta" event per content chunk.
func parseSSE(ctx context.Context, requestID string, r io.Reader) error {
	return parseSSEChunks(ctx, r, func(content string) {
		emit(requestID, types.AIChatDelta{RequestID: requestID, Delta: content})
	})
}

// parseSSEChunks consumes an OpenAI-compatible SSE stream, invoking onChunk for
// each content delta.
func parseSSEChunks(ctx context.Context, r io.Reader, onChunk func(string)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return errors.New("已停止生成")
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip keep-alive / malformed lines
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				onChunk(c.Delta.Content)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func emit(requestID string, delta types.AIChatDelta) {
	application.Get().Event.Emit("ai:delta", delta)
}

// StreamMessages streams a completion for the given messages (as-is, without
// prepending the config system prompt), invoking onChunk per content delta.
// It blocks until the stream ends. Package-level (not a frontend binding);
// used by the terminal agent to stream the final reply.
func StreamMessages(ctx context.Context, messages []types.ChatMessage, onChunk func(string)) error {
	return (&AIService{}).streamTo(ctx, messages, onChunk)
}

// streamTo performs a synchronous streaming completion with an onChunk callback.
func (s *AIService) streamTo(ctx context.Context, messages []types.ChatMessage, onChunk func(string)) error {
	cfg, err := s.GetConfig()
	if err != nil {
		return err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("尚未配置 API Key，请在「设置 → AI」中填写")
	}

	reqBody := chatRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: cfg.Temperature,
		Stream:      true,
	}
	if cfg.MaxTokens > 0 {
		reqBody.MaxTokens = cfg.MaxTokens
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("已停止生成")
		}
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("API 返回错误（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	return parseSSEChunks(ctx, resp.Body, onChunk)
}
