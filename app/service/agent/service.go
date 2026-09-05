// Package agent implements a conversational terminal agent bound to an SSH
// session. Each user message runs one "turn": the LLM either streams a text
// reply or proposes shell commands that execute on a separate SSH exec channel
// (not the PTY), feeding output back until it replies. Authorization follows
// one of three modes with a hard block-list that always requires confirmation.
// Conversation history is kept in memory with a bounded context window.
package agent

import (
	"context"
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
			"直接输出自然语言，不要输出结构化标记或代码块围栏。",
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
	return `你是一个运行在远程服务器上的终端运维助手。你可以直接回复用户，或调用工具在服务器上执行 shell 命令来观察环境并完成目标。

规则：
1. 如果用户的问题只是解释、说明、给建议（不需要实际操作服务器），直接回复文字，不要调用工具。
2. 需要实际操作服务器时，调用工具执行命令，一次只执行一条简单的单条命令。
3. 严禁把多条命令用分号、双与号、双竖线串联成一条，也不要为了分隔输出而追加一些标记文字——命令的输出和退出码会自动返回给你，你不需要额外写标记。需要多种信息就分开多次调用工具，一次只做一件事。
4. 命令必须是非交互式；不要用会进入交互界面的程序（如编辑器、分页器、资源监视器）。查看文本用文本查看类命令，分页类命令加 --no-pager 或接管道。
5. 优先只读命令观察，再逐步修改；能一步查清的不拆步。
6. 命令尽量简单稳妥，避免不必要的提权、删除、覆盖重定向。
7. 执行若干命令后，用文字给出最终结论总结给用户。`
}

// xorKey 与 deob：用硬编码密钥对加密字符串做 XOR 解密，得到原始正则源码。
// 恶意命令特征串以 XOR 密文形式存放在二进制里，运行时才还原成明文，
// 避免 "rm -rf"、"mkfs"、"curl|sh" 等以明文出现在可执行文件里被杀软误报。
var xorKey = []byte{0x4A, 0x9C, 0x2F, 0xE1, 0x76, 0x3B}

func deob(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = s[i] ^ xorKey[i%len(xorKey)]
	}
	return string(out)
}

// 硬阻断：即使「完全授权」也必须询问的命令（灾难性/不可逆）。
var hardBlockPatterns = []*regexp.Regexp{
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x38\xF1\x73\x92\x5D\x13\x67\xC7\x4E\xCC\x0C\x66\x60\xEE\x74\x80\x5B\x41\x17\xB6\x49\xBA\x17\x16\x30\xC1\x05\x9D\x5B\x49\x2C\xE0\x02\x87\x04\x12\x16\xEF\x04\xC9\x59\x47\x16\xB6\x53\x9F\x0A\x67\x64\xC0\x01\xC8")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x27\xF7\x49\x92\x2A\x59")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x2C\xF8\x46\x92\x1D\x67\x28\xE0\x73\x83\x06\x5A\x38\xE8\x4A\x85\x2A\x59\x36\xC0\x4D\x96\x1F\x4B\x2F\xFA\x5C\xBD\x14\x47\x16\xFE\x4C\x87\x12\x52\x39\xF7\x73\x83")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x2E\xF8\x73\x92\x5D\x15\x60\xF3\x49\xDC\x59\x5F\x2F\xEA\x00")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x48\x67\x39\xB6\x00\x85\x13\x4D\x65\xB4\x5C\x85\x0A\x55\x3C\xF1\x4A\x9D\x1B\x56\x29\xFE\x43\x8A\x0A\x4D\x2E\xE0\x57\x97\x12\x12")),
	regexp.MustCompile(deob("\x70\xC0\x07\xBD\x5F\x67\x39\xB6\x73\x9A\x2A\x48\x60\xA6\x73\x9D\x4C\x1D\x16\xEF\x05\xBD\x0B\x00\x75")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x39\xF4\x5A\x95\x12\x54\x3D\xF2\x73\x83\x0A\x67\x28\xEE\x4A\x83\x19\x54\x3E\xC0\x4D\x9D\x2A\x59\x3A\xF3\x58\x84\x04\x54\x2C\xFA\x73\x83\x0A\x67\x28\xF4\x4E\x8D\x02\x67\x28\xE0\x73\x83\x1F\x55\x23\xE8\x73\x92\x5D\x60\x7A\xAA\x72\xBD\x14")),
}

// 敏感命令：在「敏感操作提问」模式下需要询问。
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x39\xE9\x4B\x8E\x2A\x59")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x38\xF1\x73\x92\x5D\x16\x11\xFD\x02\x9B\x2B\x11\x11\xEE\x49\xBC\x2D\x5A\x67\xE6\x72\xCB\x2A\x59")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x29\xF4\x42\x8E\x12\x67\x39\xB7\x07\xCC\x24\x67\x39\xB7\x06\xDE\x41\x0C\x7D\xC0\x4D")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x29\xF4\x40\x96\x18\x67\x39\xB7\x02\xB3\x2A\x48\x61\xB3")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x2E\xF8\x73\x83")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x21\xF5\x43\x8D\x2A\x48\x61\xB1\x16\xBD\x14")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x2D\xF5\x5B\xBD\x05\x10\x3A\xE9\x5C\x89\x2A\x48\x61\xB4\x02\x87\x0A\x16\x67\xFA\x40\x93\x15\x5E\x63\xC0\x4D")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x2D\xF5\x5B\xBD\x05\x10\x38\xF9\x5C\x84\x02\x67\x39\xB7\x02\xCC\x1E\x5A\x38\xF8\x73\x83")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x47\x16\xEF\x05\xC9\x14\x5A\x63\xA3\x5C\x89\x2A\x59")),
	regexp.MustCompile(deob("\x62\xA3\x46\xC8\x2A\x59\x62\xFF\x5A\x93\x1A\x47\x3D\xFB\x4A\x95\x5F\x67\x28\xB2\x05\xBD\x0A")),
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
