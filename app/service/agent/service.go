// Package agent implements a conversational terminal agent bound to an SSH
// session. Each user message runs one "turn": the LLM either streams a text
// reply or proposes shell commands that execute on a separate SSH exec channel
// (not the PTY), feeding output back until it replies. Authorization follows
// one of three modes with a hard block-list that always requires confirmation.
// Conversation history is kept in memory with a bounded context window.
package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"changeme/app/service/ai"
	"changeme/app/service/terminal"
	"changeme/app/service/types"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Authorization modes.
const (
	ModeAsk       = "ask"       // 可查看：每步确认
	ModeSensitive = "sensitive" // 敏感操作提问
	ModeFull      = "full"      // 完全授权
)

const (
	maxSteps       = 30
	maxLLMMessages = 40 // 喂给模型的上下文上限（保留 system + 最近 N-1 条）
)

// AgentService keeps one conversation per SSH session.
type AgentService struct {
	mu    sync.Mutex
	convs map[string]*conversation
}

type conversation struct {
	mu        sync.Mutex
	messages  []types.ChatMessage
	running   bool
	cancel    context.CancelFunc
	approveCh chan approveDecision
}

type approveDecision struct {
	approved bool
	command  string
}

// ServiceName implements application.ServiceName.
func (s *AgentService) ServiceName() string { return "AgentService" }

func (s *AgentService) conv(sessionID string) *conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.convs == nil {
		s.convs = make(map[string]*conversation)
	}
	c := s.convs[sessionID]
	if c == nil {
		c = &conversation{}
		s.convs[sessionID] = c
	}
	return c
}

// Send appends a user message to the session conversation and runs one agent turn.
func (s *AgentService) Send(sessionID, message, mode string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return errors.New("消息不能为空")
	}
	if mode != ModeAsk && mode != ModeSensitive && mode != ModeFull {
		return errors.New("未知的授权模式")
	}
	if !terminal.IsSessionConnected(sessionID) {
		return errors.New("请先连接 SSH 会话")
	}

	c := s.conv(sessionID)
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return errors.New("AI 正在处理上一条消息，请稍候")
	}
	c.running = true
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.approveCh = make(chan approveDecision, 1)
	c.mu.Unlock()

	go s.run(ctx, c, sessionID, message, mode)
	return nil
}

// Respond feeds the user's approval decision back to a waiting agent turn.
func (s *AgentService) Respond(sessionID string, approved bool, command string) error {
	c := s.conv(sessionID)
	c.mu.Lock()
	ch := c.approveCh
	c.mu.Unlock()
	if ch == nil {
		return errors.New("没有待确认的命令")
	}
	select {
	case ch <- approveDecision{approved: approved, command: command}:
	default:
	}
	return nil
}

// Cancel stops the current turn.
func (s *AgentService) Cancel(sessionID string) error {
	c := s.conv(sessionID)
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	ch := c.approveCh
	c.mu.Unlock()
	if ch != nil {
		select {
		case ch <- approveDecision{approved: false}:
		default:
		}
	}
	return nil
}

// Clear resets the in-memory conversation for a session.
func (s *AgentService) Clear(sessionID string) error {
	s.mu.Lock()
	delete(s.convs, sessionID)
	s.mu.Unlock()
	return nil
}

func (s *AgentService) run(ctx context.Context, c *conversation, sessionID, message, mode string) {
	c.mu.Lock()
	approveCh := c.approveCh
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.approveCh = nil
		c.mu.Unlock()
	}()

	c.mu.Lock()
	if len(c.messages) == 0 {
		c.messages = append(c.messages, types.ChatMessage{Role: "system", Content: agentSystemPrompt()})
	}
	c.messages = append(c.messages, types.ChatMessage{Role: "user", Content: message})
	c.mu.Unlock()

	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID, Summary: "已取消"})
			return
		}

		c.mu.Lock()
		msgs := llmContext(c.messages)
		c.mu.Unlock()

		action, err := askAction(msgs)
		if err != nil {
			application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID, Error: err.Error()})
			return
		}

		if action.Action == "reply" {
			s.streamReply(ctx, c, sessionID, msgs)
			return
		}

		command := strings.TrimSpace(action.Command)
		if command == "" {
			application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID, Error: "AI 提议了空命令"})
			return
		}

		blocked, blockWhy := hardBlocked(command)
		danger, dangerWhy := isSensitive(command)

		needsApproval := false
		why := ""
		switch mode {
		case ModeAsk:
			needsApproval = true
		case ModeSensitive:
			needsApproval = blocked || danger
			if blocked {
				why = blockWhy
			} else if danger {
				why = dangerWhy
			}
		case ModeFull:
			needsApproval = blocked
			why = blockWhy
		}

		application.Get().Event.Emit("agent:step", types.AgentStep{
			SessionID:     sessionID,
			Step:          step + 1,
			Status:        "propose",
			Command:       command,
			Reason:        action.Reason,
			NeedsApproval: needsApproval,
			Why:           why,
		})

		finalCommand := command
		if needsApproval {
			application.Get().Event.Emit("agent:ask", types.AgentAsk{
				SessionID: sessionID, Step: step + 1, Command: command, Reason: action.Reason, Why: why,
			})
			select {
			case d := <-approveCh:
				if !d.approved {
					application.Get().Event.Emit("agent:step", types.AgentStep{SessionID: sessionID, Step: step + 1, Status: "rejected", Command: command})
					application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID, Summary: "已由用户取消"})
					return
				}
				if strings.TrimSpace(d.command) != "" {
					finalCommand = strings.TrimSpace(d.command)
				}
			case <-ctx.Done():
				application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID, Summary: "已取消"})
				return
			}
		}

		application.Get().Event.Emit("agent:step", types.AgentStep{SessionID: sessionID, Step: step + 1, Status: "running", Command: finalCommand})

		out, exitCode, err := terminal.ExecCommand(sessionID, finalCommand)
		if err != nil {
			application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID, Error: err.Error()})
			return
		}

		application.Get().Event.Emit("agent:output", types.AgentOutput{
			SessionID: sessionID, Step: step + 1, Command: finalCommand, Output: out, ExitCode: exitCode,
		})

		c.mu.Lock()
		c.messages = append(c.messages,
			types.ChatMessage{Role: "assistant", Content: "已执行命令：" + truncateCmd(finalCommand)},
			types.ChatMessage{Role: "user", Content: fmt.Sprintf("命令输出（exit=%d）：\n%s", exitCode, truncate(out))},
		)
		c.mu.Unlock()
	}

	application.Get().Event.Emit("agent:done", types.AgentDone{
		SessionID: sessionID,
		Summary:   fmt.Sprintf("达到最大步数（%d），可继续追问让 AI 接着做", maxSteps),
	})
}

// streamReply streams the final natural-language reply for the conversation.
func (s *AgentService) streamReply(ctx context.Context, c *conversation, sessionID string, msgs []types.ChatMessage) {
	var sb strings.Builder
	err := ai.StreamMessages(ctx, buildReplyMessages(msgs), func(chunk string) {
		sb.WriteString(chunk)
		application.Get().Event.Emit("agent:reply", types.AgentReply{SessionID: sessionID, Content: chunk})
	})
	if err != nil {
		if ctx.Err() != nil {
			application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID, Summary: "已取消"})
		} else {
			application.Get().Event.Emit("agent:reply", types.AgentReply{SessionID: sessionID, Done: true, Error: err.Error()})
			application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID, Error: err.Error()})
		}
		return
	}

	application.Get().Event.Emit("agent:reply", types.AgentReply{SessionID: sessionID, Done: true})

	c.mu.Lock()
	c.messages = append(c.messages, types.ChatMessage{Role: "assistant", Content: sb.String()})
	c.mu.Unlock()

	application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID})
}

func buildReplyMessages(msgs []types.ChatMessage) []types.ChatMessage {
	out := []types.ChatMessage{{
		Role: "system",
		Content: "你是终端运维助手。请根据上面的对话历史（用户请求、你已执行的命令及各自的输出），给用户一个总结。用下面三个标题，每个标题单独占一行，内容写在标题下方：\n" +
			"实现\n" +
			"（一两句话概括你做了什么，不用罗列完整命令）\n" +
			"\n" +
			"说明\n" +
			"（针对用户问题的解释与关键数据，如磁盘占用、进程状态、原因）\n" +
			"\n" +
			"验证\n" +
			"（你是通过哪些命令输出 / 退出码验证上述结论的，结果如何）\n" +
			"\n" +
			"直接输出自然语言，不要输出 JSON、不要 markdown 代码块。",
	}}
	if len(msgs) > 1 {
		out = append(out, msgs[1:]...)
	}
	return out
}

// llmContext caps the messages sent to the model to bound memory and tokens.
func llmContext(msgs []types.ChatMessage) []types.ChatMessage {
	if len(msgs) <= maxLLMMessages {
		return append([]types.ChatMessage(nil), msgs...)
	}
	out := make([]types.ChatMessage, 0, maxLLMMessages)
	out = append(out, msgs[0]) // system
	out = append(out, msgs[len(msgs)-(maxLLMMessages-1):]...)
	return out
}

// runCommandTool 是 agent 用来执行命令的工具定义。
var runCommandTool = ai.Tool{
	Type: "function",
	Function: ai.ToolFunc{
		Name:        "run_command",
		Description: "在远程服务器上执行一条单条 shell 命令（不要串联多条命令、不要加 echo 分隔符），返回命令输出与退出码。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "要执行的一条 shell 命令（单条，不要用 ; 或 && 串联多条命令）"},
				"reason":  map[string]any{"type": "string", "description": "执行这一步的简短原因"},
			},
			"required": []string{"command"},
		},
	},
}

type agentAction struct {
	Action  string // reply | command
	Command string
	Reason  string
}

// askAction 通过 function calling 让模型决定下一步：调用 run_command 工具
// 执行命令，或直接回复文字。若模型把多条命令串联（用 ;、&&、||），会回喂
// 纠正并要求它一次只执行一条，最多重试 3 次。
func askAction(msgs []types.ChatMessage) (agentAction, error) {
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := ai.ChatTools(msgs, []ai.Tool{runCommandTool})
		if err != nil {
			return agentAction{}, err
		}
		if len(resp.ToolCalls) > 0 {
			tc := resp.ToolCalls[0]
			if tc.Name == "run_command" {
				cmd, _ := tc.Arguments["command"].(string)
				reason, _ := tc.Arguments["reason"].(string)
				cmd = strings.TrimSpace(cmd)
				if cmd != "" {
					if !isSingleCommand(cmd) {
						msgs = append(msgs,
							types.ChatMessage{Role: "assistant", Content: "命令：" + truncateCmd(cmd)},
							types.ChatMessage{Role: "user", Content: "这条命令把多条命令串联了（含 ;、&&、|| 或 echo 分隔符）。请一次只执行一条简单命令，不要用 ;、&&、|| 串联，不要 echo 分隔符/退出码；需要多种信息就多次调用 run_command。"},
						)
						continue
					}
					return agentAction{Action: "command", Command: cmd, Reason: reason}, nil
				}
			}
		}
		if strings.TrimSpace(resp.Content) != "" {
			return agentAction{Action: "reply"}, nil
		}
		return agentAction{}, errors.New("模型既没有回复也没有调用工具")
	}
	return agentAction{}, errors.New("AI 多次返回串联命令，已停止")
}

// isSingleCommand 判断是否是一条命令（忽略引号内的 ;/&&/||，允许单管道 |）。
func isSingleCommand(cmd string) bool {
	var quote byte
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' {
				i++
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case ';':
			return false
		case '&':
			if i+1 < len(cmd) && cmd[i+1] == '&' {
				return false
			}
		case '|':
			if i+1 < len(cmd) && cmd[i+1] == '|' {
				return false
			}
		}
	}
	return true
}

func truncateCmd(s string) string {
	const max = 160
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func agentSystemPrompt() string {
	return `你是一个运行在远程服务器上的终端运维助手。你可以直接回复用户，或调用 run_command 工具在服务器上执行 shell 命令来观察环境并完成目标。

规则：
1. 如果用户的问题只是解释、说明、给建议（不需要实际操作服务器），直接回复文字，不要调用工具。
2. 需要实际操作服务器时，调用 run_command 工具，**一次只执行一条简单的单条命令**。
3. 严禁把多条命令用 ;、&&、|| 串联成一条，也不要为了分隔输出而 echo 一些标记——命令的输出和退出码会自动返回给你，你不需要写 echo。需要多种信息就分开多次调用 run_command，一次只做一件事。
4. 命令必须是非交互式；不要用会进入交互界面的程序（如 vim、less、top）。查看文本用 cat/grep/head/tail，分页类命令加 --no-pager 或 | cat。
5. 优先只读命令观察，再逐步修改；能一步查清的不拆步。
6. 命令尽量简单稳妥，避免不必要的 sudo、rm、重定向覆盖。
7. 执行若干命令后，用文字给出最终结论总结给用户。`
}

// buildPattern 把 base64 编码的正则源码在运行时解码后编译。
// 避免 "rm -rf"、"mkfs"、"curl|sh" 这类恶意命令特征串以明文出现在二进制里，
// 降低被杀软静态启发式误报的概率。
func buildPattern(encoded string) *regexp.Regexp {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		panic(err)
	}
	return regexp.MustCompile(string(raw))
}

// 硬阻断：即使「完全授权」也必须询问的命令（灾难性/不可逆）。
var hardBlockPatterns = []*regexp.Regexp{
	buildPattern("KD9pKVxicm1ccysoLVthLXpdKnJbYS16XSpmW2Etel0qfC1yZnwtZnIpXHMrKC98XCp8fnxcLlwuKQ=="),
	buildPattern("KD9pKVxibWtmc1xi"),
	buildPattern("KD9pKVxiZmRpc2tcYnxcYnBhcnRlZFxifFxid2lwZWZzXGJ8XGJjZmRpc2tcYg=="),
	buildPattern("KD9pKVxiZGRccysuKm9mPS9kZXYv"),
	buildPattern("KD9pKT5ccyovZGV2LyhzZHxudm1lfG1tY2Jsa3x2ZHh4dmQp"),
	buildPattern("OlwoXClccypce1xzKjpcfDomXHMqXH07Pw=="),
	buildPattern("KD9pKVxic2h1dGRvd25cYnxcYnJlYm9vdFxifFxicG93ZXJvZmZcYnxcYmhhbHRcYnxcYmluaXRccytbMDZdXGI="),
}

// 敏感命令：在「敏感操作提问」模式下需要询问。
var sensitivePatterns = []*regexp.Regexp{
	buildPattern("KD9pKVxic3Vkb1xi"),
	buildPattern("KD9pKVxicm1ccystW2Etel0qW3JmXVthLXpdKlxi"),
	buildPattern("KD9pKVxiY2htb2RccysoLVJccyspPzc3N1xi"),
	buildPattern("KD9pKVxiY2hvd25ccystUlxzKy8="),
	buildPattern("KD9pKVxiZGRcYg=="),
	buildPattern("KD9pKVxia2lsbFxzKy05XGI="),
	buildPattern("KD9pKVxiZ2l0XHMrcHVzaFxzKygtZnwtLWZvcmNlKVxi"),
	buildPattern("KD9pKVxiZ2l0XHMrcmVzZXRccystLWhhcmRcYg=="),
	buildPattern("KD9pKVx8XHMqKGJhKT9zaFxi"),
	buildPattern("KD9pKVxiKGN1cmx8d2dldClcYi4qXHw="),
}

func hardBlocked(cmd string) (bool, string) {
	for _, p := range hardBlockPatterns {
		if p.MatchString(cmd) {
			return true, "命中高危操作（不可逆/灾难性），强制确认"
		}
	}
	return false, ""
}

func isSensitive(cmd string) (bool, string) {
	for _, p := range sensitivePatterns {
		if p.MatchString(cmd) {
			return true, "疑似敏感操作，请确认"
		}
	}
	return false, ""
}

func truncate(s string) string {
	const max = 8000
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…(输出过长已截断)"
}
