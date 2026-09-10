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

// ---------- 消息与工具类型 ----------

// Message is one turn in a chat conversation. Compared with the frontend-facing
// DTO types.ChatMessage it additionally carries the fields the OpenAI
// tool-calling protocol defines: an assistant turn may request tool_calls, and
// every tool turn must echo the matching tool_call_id. Feeding tool results
// back through the real protocol (instead of faking them as chat text) is what
// keeps tool use reliable across providers.
type Message struct {
	Role       string     `json:"role"` // system | user | assistant | tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// SystemMessage / UserMessage / AssistantMessage build plain text turns.
func SystemMessage(content string) Message    { return Message{Role: "system", Content: content} }
func UserMessage(content string) Message      { return Message{Role: "user", Content: content} }
func AssistantMessage(content string) Message { return Message{Role: "assistant", Content: content} }

// AssistantToolCalls builds the assistant turn that requested the given tools.
// The calls must be echoed back verbatim so the matching tool results line up.
func AssistantToolCalls(calls []ToolCall) Message {
	return Message{Role: "assistant", ToolCalls: calls}
}

// ToolResult builds the tool turn carrying one tool call's output back.
func ToolResult(callID, content string) Message {
	return Message{Role: "tool", ToolCallID: callID, Content: content}
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

// ToolCall is one tool call in wire format (arguments is a JSON string).
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction is the name plus the raw JSON arguments of one tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ParsedToolCall is a tool call with its arguments already decoded.
type ParsedToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolResponse is one assistant turn returned by a tool-calling request:
// the natural-language content (possibly empty), the decoded tool calls, and
// the raw wire calls to append to the conversation history.
type ToolResponse struct {
	Content   string
	ToolCalls []ParsedToolCall
	RawCalls  []ToolCall
}

// Options tunes a single request. Nil / zero fields fall back to the values
// stored in the AI settings.
type Options struct {
	Temperature *float64 // nil → 用设置里的温度
	MaxTokens   int      // <=0 → 用设置里的上限
}

// Temp is a helper for building Options with an explicit temperature, since a
// pointer is needed to distinguish "0" from "not set".
func Temp(v float64) *float64 { return &v }

// ---------- 配置 ----------

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

// ListModels fetches the model IDs available on the configured provider
// (OpenAI-compatible GET /models). Used to populate the model selector instead
// of a hard-coded list.
func (s *AIService) ListModels() ([]string, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return nil, err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	setAuth(req, apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("获取模型列表失败（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// ---------- 对外补全接口 ----------

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

	// Prepend the system prompt when configured.
	msgs := make([]Message, 0, len(messages)+1)
	if cfg.SystemPrompt != "" {
		msgs = append(msgs, SystemMessage(cfg.SystemPrompt))
	}
	for _, m := range messages {
		msgs = append(msgs, Message{Role: m.Role, Content: m.Content})
	}

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
	msgs := []Message{}
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, SystemMessage(systemPrompt))
	}
	msgs = append(msgs, UserMessage(userContent))
	return s.complete(msgs, false, nil)
}

// ChatJSON runs a non-streaming completion in JSON output mode and returns
// the assistant's raw content. Package-level (not bound to the frontend),
// used internally by the terminal agent.
func ChatJSON(messages []Message) (string, error) {
	return (&AIService{}).complete(messages, true, nil)
}

// ChatTools runs a non-streaming completion with the given tool definitions
// and returns the assistant's text content and/or tool calls. Package-level
// (not bound to the frontend); used by the terminal agent.
func ChatTools(messages []Message, tools []Tool, opts *Options) (ToolResponse, error) {
	return (&AIService{}).chatTools(messages, tools, opts)
}

// StreamMessages streams a completion for the given messages (as-is, without
// prepending the config system prompt), invoking onChunk per content delta.
// It blocks until the stream ends. Package-level (not a frontend binding);
// used by the terminal agent to stream the final reply.
func StreamMessages(ctx context.Context, messages []Message, opts *Options, onChunk func(string)) error {
	return (&AIService{}).streamTo(ctx, messages, opts, onChunk)
}

// StreamTools runs a streaming completion with tool definitions. Text deltas
// are handed to onContent as they arrive (so the UI can type out the model's
// narration while it decides), and the fully assembled assistant turn — text
// plus tool calls — is returned once the stream ends. Streaming the decision
// costs nothing extra and removes the long silent pause a non-streaming
// tool call would otherwise cause.
func StreamTools(
	ctx context.Context,
	messages []Message,
	tools []Tool,
	opts *Options,
	onContent func(string),
) (ToolResponse, error) {
	return (&AIService{}).streamTools(ctx, messages, tools, opts, onContent)
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

	msgs := []Message{}
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, SystemMessage(systemPrompt))
	}
	msgs = append(msgs, UserMessage(userContent))

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

// setAuth attaches the bearer token only when a key is present. Local,
// keyless OpenAI-compatible servers (LM Studio / llama.cpp / Ollama, etc.)
// accept requests without an Authorization header, so a missing key must not
// block the request — only remote providers that require a key will reject it.
func setAuth(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// resolveRequest merges per-request options over the stored configuration.
func resolveRequest(cfg types.AIConfig, opts *Options) (temperature float64, maxTokens int) {
	temperature, maxTokens = cfg.Temperature, cfg.MaxTokens
	if opts != nil {
		if opts.Temperature != nil {
			temperature = *opts.Temperature
		}
		if opts.MaxTokens > 0 {
			maxTokens = opts.MaxTokens
		}
	}
	return temperature, maxTokens
}

// ---------- 请求实现 ----------

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Stream         bool            `json:"stream"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

// toolRequest is the request body for a tool-calling completion.
type toolRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  string    `json:"tool_choice,omitempty"`
}

func (s *AIService) chatTools(messages []Message, tools []Tool, opts *Options) (ToolResponse, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return ToolResponse{}, err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return ToolResponse{}, err
	}
	temperature, maxTokens := resolveRequest(cfg, opts)

	reqBody := toolRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: temperature,
		Stream:      false,
		Tools:       tools,
		ToolChoice:  "auto",
	}
	if maxTokens > 0 {
		reqBody.MaxTokens = maxTokens
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
	setAuth(req, apiKey)

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
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
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
	result.RawCalls = out.Choices[0].Message.ToolCalls
	for _, tc := range result.RawCalls {
		result.ToolCalls = append(result.ToolCalls, parseToolCall(tc))
	}
	return result, nil
}

// streamTools runs the tool-calling request in streaming mode, accumulating
// content and tool-call fragments (providers deliver tool call arguments in
// pieces indexed by position).
func (s *AIService) streamTools(
	ctx context.Context,
	messages []Message,
	tools []Tool,
	opts *Options,
	onContent func(string),
) (ToolResponse, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return ToolResponse{}, err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return ToolResponse{}, err
	}
	temperature, maxTokens := resolveRequest(cfg, opts)

	reqBody := toolRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: temperature,
		Stream:      true,
		Tools:       tools,
		ToolChoice:  "auto",
	}
	if maxTokens > 0 {
		reqBody.MaxTokens = maxTokens
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return ToolResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ToolResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuth(req, apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ToolResponse{}, errors.New("已停止生成")
		}
		return ToolResponse{}, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ToolResponse{}, fmt.Errorf("API 返回错误（HTTP %d）: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var content strings.Builder
	type callAcc struct {
		id   string
		name string
		args strings.Builder
	}
	accs := map[int]*callAcc{}
	order := []int{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
scan:
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ToolResponse{}, errors.New("已停止生成")
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
			break scan
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int              `json:"index"`
						ID       string           `json:"id"`
						Type     string           `json:"type"`
						Function ToolCallFunction `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip keep-alive / malformed lines
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				content.WriteString(c.Delta.Content)
				if onContent != nil {
					onContent(c.Delta.Content)
				}
			}
			for _, tc := range c.Delta.ToolCalls {
				acc := accs[tc.Index]
				if acc == nil {
					acc = &callAcc{}
					accs[tc.Index] = acc
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				acc.args.WriteString(tc.Function.Arguments)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ToolResponse{}, err
	}

	out := ToolResponse{Content: content.String()}
	for _, idx := range order {
		acc := accs[idx]
		if acc == nil {
			continue
		}
		call := ToolCall{
			ID:       acc.id,
			Type:     "function",
			Function: ToolCallFunction{Name: acc.name, Arguments: acc.args.String()},
		}
		out.RawCalls = append(out.RawCalls, call)
		out.ToolCalls = append(out.ToolCalls, parseToolCall(call))
	}
	return out, nil
}

// parseToolCall decodes the raw JSON arguments of one wire tool call. A call
// whose arguments are not valid JSON still comes back with its name and id, so
// the agent can report the problem through the normal tool-result channel
// instead of aborting the whole turn.
func parseToolCall(tc ToolCall) ParsedToolCall {
	args := map[string]any{}
	if raw := strings.TrimSpace(tc.Function.Arguments); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			args = map[string]any{}
		}
	}
	return ParsedToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: args}
}

// complete performs a non-streaming chat completion and returns the assistant
// content. jsonMode requests JSON output (response_format).
func (s *AIService) complete(messages []Message, jsonMode bool, opts *Options) (string, error) {
	cfg, err := s.GetConfig()
	if err != nil {
		return "", err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return "", err
	}
	temperature, maxTokens := resolveRequest(cfg, opts)

	reqBody := chatRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: temperature,
		Stream:      false,
	}
	if maxTokens > 0 {
		reqBody.MaxTokens = maxTokens
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
	setAuth(req, apiKey)

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

func (s *AIService) stream(requestID string, cfg types.AIConfig, apiKey string, msgs []Message) {
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

// streamTo performs a synchronous streaming completion with an onChunk callback.
func (s *AIService) streamTo(ctx context.Context, messages []Message, opts *Options, onChunk func(string)) error {
	cfg, err := s.GetConfig()
	if err != nil {
		return err
	}
	apiKey, err := s.apiKey()
	if err != nil {
		return err
	}
	temperature, maxTokens := resolveRequest(cfg, opts)

	reqBody := chatRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: temperature,
		Stream:      true,
	}
	if maxTokens > 0 {
		reqBody.MaxTokens = maxTokens
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
	setAuth(req, apiKey)

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

func (s *AIService) doStream(ctx context.Context, requestID string, cfg types.AIConfig, apiKey string, msgs []Message) error {
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
	setAuth(req, apiKey)

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
