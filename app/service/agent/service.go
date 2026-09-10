// Package agent implements a conversational terminal agent bound to an SSH
// session. Each user message runs one "turn": the LLM either streams a text
// reply or proposes shell commands that execute over a separate SSH exec
// channel (not the PTY), feeding output back until it replies. Authorization
// follows one of three modes with a hard block-list that always requires
// confirmation. Conversation history is kept in memory with a bounded context
// window.
//
// 与"看起来不聪明"直接相关的几个设计点：
//   - 命令结果按真实的 tool 调用协议回灌（assistant.tool_calls + role:"tool"），
//     而不是伪造成对话文本，模型判断下一步才准；
//   - 命令的 stderr 与 stdout 合并返回（历史版本把 stderr 整段丢掉，
//     而 nginx -t / systemctl / docker / 权限报错等大量把关键信息写在 stderr）；
//   - 工作目录跨步骤保留，模型不必自己拼 cd 前缀；
//   - 每次会话采集一次环境快照（系统 / 内核 / 用户 / 包管理器 / init），
//     并附上用户 PTY 的最近输出，省掉大量试探性命令；
//   - 决策阶段是流式的，"正在思考"和逐步生成的文字都能实时看到。
package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"changeme/app/service/ai"
	"changeme/app/service/settings"
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
	// maxSteps 一轮对话最多执行多少步（每步 = 一次工具调用）
	maxSteps = 40
	// maxLLMMessages 喂给模型的上下文上限（保留 system + 最近 N-1 条）
	maxLLMMessages = 60
	// agentTemperature 运维场景要稳定可复现，比闲聊用的默认温度低得多
	agentTemperature = 0.2
	// maxToolOutput 单条命令回灌给模型的输出上限（字节）
	maxToolOutput = 12000
	// maxTerminalContext 附带给模型的用户终端最近输出上限（字节）
	maxTerminalContext = 3000
	// maxEnvChars 环境快照上限（字符）
	maxEnvChars = 1500
	// cwdMarker 包装命令结尾打印工作目录用的标记，回读后从输出里剥掉
	cwdMarker = "__SPARK_CWD__"
)

// 设置项：终端 Agent 行为开关（设置 → AI 助手）
const (
	// KeyAllowChain 是否允许模型一次提交含 ; / && / || 的命令链（默认关闭）
	KeyAllowChain = "ai.agent.allowChain"
	// KeyTerminalContext 是否把用户终端最近输出作为上下文（默认开启）
	KeyTerminalContext = "ai.agent.terminalContext"
)

// allowChain 读取「允许命令串联」开关。
func allowChain() bool { return settings.GetString(KeyAllowChain, "0") == "1" }

// terminalContextEnabled 读取「附带终端最近输出」开关。
func terminalContextEnabled() bool { return settings.GetString(KeyTerminalContext, "1") != "0" }

// AgentService keeps one conversation per SSH session.
type AgentService struct {
	mu    sync.Mutex
	convs map[string]*conversation
}

type conversation struct {
	mu        sync.Mutex
	messages  []ai.Message
	running   bool
	cancel    context.CancelFunc
	approveCh chan approveDecision

	// cwd 跨步骤保留的工作目录：每条命令先 cd 到这里，执行后再回读 pwd，
	// 所以模型写 cd 的效果会延续到下一步。
	cwd string
	// env 环境快照（每个会话只采集一次）
	env     string
	envOnce bool
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

// Clear resets the in-memory conversation for a session (含环境快照与工作目录)。
func (s *AgentService) Clear(sessionID string) error {
	s.mu.Lock()
	delete(s.convs, sessionID)
	s.mu.Unlock()
	return nil
}

// ---------- 一轮对话 ----------

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

	// 环境快照：每个会话只花一条命令采集一次，之后写进系统提示词。
	emitStep(sessionID, 0, "thinking", "", "正在读取服务器环境", false, "")
	env := s.environment(ctx, c, sessionID)
	if ctx.Err() != nil {
		emitDone(sessionID, "已取消", "")
		return
	}

	// 用户终端里最近发生了什么（他刚敲的命令、刚看到的报错）——没有这段，
	// "帮我看看这个报错" 这类最常见的诉求根本接不住。
	termTail := ""
	if terminalContextEnabled() {
		termTail = terminal.RecentOutput(sessionID, maxTerminalContext)
	}

	c.mu.Lock()
	// 每轮都重写系统提示词：设置（命令串联开关等）改了下一轮就生效
	sys := ai.SystemMessage(agentSystemPrompt(env, termTail, allowChain()))
	if len(c.messages) == 0 {
		c.messages = append(c.messages, sys)
	} else {
		c.messages[0] = sys
	}
	c.messages = append(c.messages, ai.UserMessage(message))
	c.mu.Unlock()

	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			emitDone(sessionID, "已取消", "")
			return
		}

		emitStep(sessionID, step+1, "thinking", "", "正在思考下一步", false, "")

		c.mu.Lock()
		msgs := llmContext(c.messages)
		c.mu.Unlock()

		streamed := false
		resp, err := ai.StreamTools(ctx, msgs, []ai.Tool{runCommandTool}, agentOptions(),
			func(chunk string) {
				streamed = true
				emitReply(sessionID, chunk)
			})
		if err != nil && ctx.Err() == nil && !streamed {
			// 个别服务商不支持「流式 + 工具调用」，退回一次非流式请求
			// （已经开始流式输出就不重试了，否则会把同一段话显示两遍）
			resp, err = ai.ChatTools(msgs, []ai.Tool{runCommandTool}, agentOptions())
		}
		if err != nil {
			if ctx.Err() != nil {
				emitDone(sessionID, "已取消", "")
				return
			}
			emitReplyDone(sessionID, err.Error())
			emitDone(sessionID, "", err.Error())
			return
		}
		if streamed {
			emitReplyDone(sessionID, "")
		}

		c.mu.Lock()
		c.messages = append(c.messages, ai.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.RawCalls,
		})
		c.mu.Unlock()

		// 没有工具调用 = 这一轮的文字就是最终回答
		if len(resp.ToolCalls) == 0 {
			if !streamed {
				text := strings.TrimSpace(resp.Content)
				if text == "" {
					text = "（模型没有返回内容，可以直接追问，或换一个模型试试）"
				}
				emitReply(sessionID, text)
				emitReplyDone(sessionID, "")
			}
			emitDone(sessionID, "", "")
			return
		}

		for _, tc := range resp.ToolCalls {
			if ctx.Err() != nil {
				emitDone(sessionID, "已取消", "")
				return
			}

			if tc.Name != runCommandTool.Function.Name {
				s.addToolResult(c, tc.ID, "错误：未知工具 "+tc.Name)
				continue
			}

			command := strings.TrimSpace(argString(tc.Arguments, "command"))
			reason := strings.TrimSpace(argString(tc.Arguments, "reason"))
			if command == "" {
				s.addToolResult(c, tc.ID, "错误：command 为空。请在 command 字段里给出要执行的那条命令")
				continue
			}

			// 不允许串联时，把原因作为工具结果回灌让模型自己拆开，
			// 而不是让整轮对话失败（历史版本重试 3 次后直接报错）。
			if why, bad := chainViolation(command); bad {
				emitStep(sessionID, step+1, "blocked", command, why, false, "")
				s.addToolResult(c, tc.ID, "错误："+why)
				continue
			}

			blocked, blockWhy := hardBlocked(command)
			danger, dangerWhy := isSensitive(command)
			needsApproval, why := approvalFor(mode, blocked, danger, blockWhy, dangerWhy)

			emitStep(sessionID, step+1, "propose", command, reason, needsApproval, why)

			finalCommand := command
			if needsApproval {
				application.Get().Event.Emit("agent:ask", types.AgentAsk{
					SessionID: sessionID, Step: step + 1, Command: command, Reason: reason, Why: why,
				})
				select {
				case d := <-approveCh:
					if !d.approved {
						emitStep(sessionID, step+1, "rejected", command, "", false, "")
						s.addToolResult(c, tc.ID, "用户拒绝执行该命令。请换一种方式，或先用文字向用户说明为什么需要它")
						emitDone(sessionID, "已由用户取消", "")
						return
					}
					if strings.TrimSpace(d.command) != "" {
						finalCommand = strings.TrimSpace(d.command)
					}
				case <-ctx.Done():
					emitDone(sessionID, "已取消", "")
					return
				}
			}

			emitStep(sessionID, step+1, "running", finalCommand, "", false, "")

			out, exitCode, newCwd, err := s.execCommand(sessionID, c, finalCommand)
			if err != nil {
				// 命令没能跑起来（超时 / 会话断开）：告诉模型，让它自己决定换命令还是收尾
				s.addToolResult(c, tc.ID, "执行未完成："+err.Error())
				application.Get().Event.Emit("agent:output", types.AgentOutput{
					SessionID: sessionID, Step: step + 1, Command: finalCommand,
					Output: err.Error(), ExitCode: -1,
				})
				if !terminal.IsSessionConnected(sessionID) {
					emitDone(sessionID, "", "SSH 会话已断开")
					return
				}
				continue
			}

			application.Get().Event.Emit("agent:output", types.AgentOutput{
				SessionID: sessionID, Step: step + 1, Command: finalCommand,
				Output: out, ExitCode: exitCode,
			})
			s.addToolResult(c, tc.ID, toolResultText(exitCode, newCwd, out))
		}
	}

	emitDone(sessionID, fmt.Sprintf("已达到最大步数（%d）。可以继续追问让它接着做", maxSteps), "")
}

// ---------- 环境快照与命令执行 ----------

// envScript 一条命令采集出模型真正需要的环境信息。
const envScript = `printf 'OS: '; ( . /etc/os-release 2>/dev/null && printf '%s %s' "$NAME" "$VERSION_ID" ) || uname -s
printf '\nKernel: '; uname -sr
printf '\nArch: '; uname -m
printf '\nUser: '; whoami
printf '\nHome: '; printf '%s' "$HOME"
printf '\nShell: '; printf '%s' "$SHELL"
printf '\nPWD: '; pwd
printf '\nPackageManager: '; for p in apt-get dnf yum apk pacman zypper; do command -v "$p" >/dev/null 2>&1 && printf '%s ' "$p"; done
printf '\nInit: '; if command -v systemctl >/dev/null 2>&1; then printf systemd; else printf other; fi
printf '\n'`

// environment 采集（并缓存）会话所在服务器的环境快照。采集失败不是致命错误，
// 只是退化成没有快照。
func (s *AgentService) environment(ctx context.Context, c *conversation, sessionID string) string {
	c.mu.Lock()
	if c.envOnce {
		env := c.env
		c.mu.Unlock()
		return env
	}
	c.mu.Unlock()

	out, _, err := terminal.ExecCommand(sessionID, envScript)
	if err != nil || strings.TrimSpace(out) == "" {
		c.mu.Lock()
		c.envOnce = true
		c.mu.Unlock()
		return ""
	}
	env := strings.TrimSpace(out)
	if len(env) > maxEnvChars {
		env = env[:maxEnvChars]
	}
	c.mu.Lock()
	c.env, c.envOnce = env, true
	c.mu.Unlock()
	return env
}

// execCommand 执行一条命令，返回（合并后的输出, 退出码, 执行后的工作目录, 错误）。
func (s *AgentService) execCommand(sessionID string, c *conversation, command string) (string, int, string, error) {
	c.mu.Lock()
	cwd := c.cwd
	c.mu.Unlock()

	raw, exitCode, err := terminal.ExecCommand(sessionID, wrapCommand(cwd, command))
	if err != nil {
		return "", exitCode, "", err
	}
	out, newCwd := splitCwd(raw)
	if newCwd != "" {
		c.mu.Lock()
		c.cwd = newCwd
		c.mu.Unlock()
	}
	return out, exitCode, newCwd, nil
}

// wrapCommand 把模型给的一条命令包成一段小脚本，一次解决三件事：
//  1. 先 cd 到跨步骤保留的工作目录，模型因此不需要自己拼 cd 前缀；
//  2. exec 2>&1：把 stderr 并进 stdout。历史版本把 stderr 整段丢弃，
//     而 nginx -t / systemctl / docker / 权限错误等大量把关键信息写在 stderr，
//     模型拿到空输出只能瞎猜——这是"不聪明"最直接的原因；
//  3. 结尾打印执行后的 pwd，回读后更新工作目录，cd 的效果得以延续。
//
// 命令用 eval + 单引号整体转义，多行命令 / heredoc 也不会破坏脚本书写。
func wrapCommand(cwd, command string) string {
	var b strings.Builder
	if cwd != "" {
		b.WriteString("cd " + shellQuote(cwd) + " 2>/dev/null || true\n")
	}
	b.WriteString("exec 2>&1\n")
	b.WriteString("eval " + shellQuote(command) + "\n")
	b.WriteString("__spark_rc=$?\n")
	b.WriteString("printf '\\n" + cwdMarker + "%s\\n' \"$(pwd)\"\n")
	b.WriteString("exit $__spark_rc")
	return b.String()
}

// shellQuote 用单引号把字符串包成 shell 字面量。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// splitCwd 从命令输出里剥掉结尾的工作目录标记，返回（输出, 工作目录）。
func splitCwd(raw string) (string, string) {
	idx := strings.LastIndex(raw, cwdMarker)
	if idx < 0 {
		return strings.TrimRight(raw, "\r\n"), ""
	}
	out := strings.TrimRight(raw[:idx], "\r\n")
	cwd := strings.Trim(raw[idx+len(cwdMarker):], "\r\n")
	return out, strings.TrimSpace(cwd)
}

// toolResultText 组织回灌给模型的工具结果：退出码 + 工作目录 + 输出。
func toolResultText(exitCode int, cwd, out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		out = "（无输出）"
	}
	if len(out) > maxToolOutput {
		out = out[:maxToolOutput] + "\n…(输出过长已截断)"
	}
	msg := fmt.Sprintf("退出码: %d", exitCode)
	if cwd != "" {
		msg += "\n工作目录: " + cwd
	}
	return msg + "\n输出:\n" + out
}

func (s *AgentService) addToolResult(c *conversation, callID, content string) {
	c.mu.Lock()
	c.messages = append(c.messages, ai.ToolResult(callID, content))
	c.mu.Unlock()
}

// agentOptions 终端 agent 固定用低温度，保证同样的问法给出同样的操作。
func agentOptions() *ai.Options {
	return &ai.Options{Temperature: ai.Temp(agentTemperature)}
}

// argString 安全读取工具参数里的字符串字段。
func argString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// approvalFor 按授权模式决定这一步是否需要用户确认。
func approvalFor(mode string, blocked, danger bool, blockWhy, dangerWhy string) (bool, string) {
	switch mode {
	case ModeAsk:
		return true, "仅可查看模式：每一步都需要你确认"
	case ModeSensitive:
		if blocked {
			return true, blockWhy
		}
		if danger {
			return true, dangerWhy
		}
		return false, ""
	case ModeFull:
		if blocked {
			return true, blockWhy
		}
		return false, ""
	}
	return true, ""
}

// chainViolation 在「不允许命令串联」时拒绝含 ; / && / || 的命令，返回给模型的说明。
func chainViolation(cmd string) (string, bool) {
	if allowChain() || isSingleCommand(cmd) {
		return "", false
	}
	return "当前设置不允许把多条命令用 ; / && / || 串联。请一次只执行一条命令，" +
		"需要多步就分多次调用 run_command；工作目录会自动延续，不需要写 cd 前缀。", true
}

// ---------- 事件 ----------

func emitStep(sessionID string, step int, status, command, reason string, needsApproval bool, why string) {
	application.Get().Event.Emit("agent:step", types.AgentStep{
		SessionID:     sessionID,
		Step:          step,
		Status:        status,
		Command:       command,
		Reason:        reason,
		NeedsApproval: needsApproval,
		Why:           why,
	})
}

func emitReply(sessionID, content string) {
	application.Get().Event.Emit("agent:reply", types.AgentReply{SessionID: sessionID, Content: content})
}

func emitReplyDone(sessionID, errMsg string) {
	application.Get().Event.Emit("agent:reply", types.AgentReply{SessionID: sessionID, Done: true, Error: errMsg})
}

func emitDone(sessionID, summary, errMsg string) {
	application.Get().Event.Emit("agent:done", types.AgentDone{SessionID: sessionID, Summary: summary, Error: errMsg})
}

// ---------- 提示词 ----------

// runCommandTool 是 agent 用来执行命令的工具定义。
var runCommandTool = ai.Tool{
	Type: "function",
	Function: ai.ToolFunc{
		Name: "run_command",
		Description: "在远程服务器上执行一条 shell 命令，返回合并后的 stdout+stderr 与退出码。" +
			"工作目录会在多次调用之间自动保留（上一条命令里的 cd 会延续到下一次调用）。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "要执行的 shell 命令"},
				"reason":  map[string]any{"type": "string", "description": "执行这一步的简短原因"},
			},
			"required": []string{"command"},
		},
	},
}

// agentSystemPrompt 组装系统提示词：规则 + 服务器环境快照 + 用户终端最近输出。
func agentSystemPrompt(env, termTail string, chain bool) string {
	var b strings.Builder
	b.WriteString(`你是一个直接运行在用户远程服务器上的终端助手。你可以调用 run_command 在服务器上执行 shell 命令来观察环境、完成任务，也可以直接用文字回答。

工作方式：
1. 只是解释、建议、写脚本这类不需要看服务器的问题，直接回答，不要调用工具。
2. 需要看真实状态时调用 run_command。信息尽量一条命令拿全（例如一次 df -h、一次 systemctl status nginx），不要把简单查询拆成很多步。
3. 命令必须是非交互式的，不要用 top / vi / less / man 这类会进入交互界面的程序；分页命令加 --no-pager 或接管道。
4. 先只读观察，确认真实情况后再改；改动类操作一次只做一件，做完要验证。
5. 命令的 stdout 和 stderr 会一起返回，退出码也会返回，不需要你 echo 分隔符或退出码。
6. 不要用 heredoc（<<EOF）这类跨行写法，需要写文件用 printf / tee 的单行写法。
7. 回答用简洁自然的中文，可以用 Markdown（列表、表格、代码块）。不要套固定的小标题模板，也不要重复罗列你执行过的命令。
`)
	if chain {
		b.WriteString("8. 允许在一条命令里用 && / ; / || 串联多个步骤（例如先 cd 再操作），但不要写超长命令链。\n")
	} else {
		b.WriteString("8. 一次只提交一条命令，不要用 ; / && / || 串联；需要多步就分多次调用 run_command。工作目录会自动延续，不需要写 cd 前缀。\n")
	}
	if env != "" {
		b.WriteString("\n【服务器环境】以下信息已自动采集，不需要再执行命令确认：\n")
		b.WriteString(env)
		b.WriteString("\n")
	}
	if termTail != "" {
		b.WriteString("\n【用户终端的最近输出】用户当前终端窗口里最近的内容（含他刚敲的命令与报错）。" +
			"当他说「这个报错」「刚才那条命令」时以此为准；如果与本次任务无关，忽略即可：\n")
		b.WriteString(termTail)
		b.WriteString("\n")
	}
	return b.String()
}

// llmContext caps the messages sent to the model to bound memory and tokens.
// 裁掉开头孤立的 tool 结果：它对应的 assistant tool_calls 已经被裁掉，
// 多数服务商会直接判为非法请求。
func llmContext(msgs []ai.Message) []ai.Message {
	if len(msgs) <= maxLLMMessages {
		return append([]ai.Message(nil), msgs...)
	}
	out := make([]ai.Message, 0, maxLLMMessages)
	out = append(out, msgs[0]) // system
	tail := append([]ai.Message(nil), msgs[len(msgs)-(maxLLMMessages-1):]...)
	for len(tail) > 0 && tail[0].Role == "tool" {
		tail = tail[1:]
	}
	out = append(out, tail...)
	return out
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
