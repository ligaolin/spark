// AI 助手统一入口：集中维护各类任务的提示词模板，调用后端补全
// （一次性 AIService.Complete / 流式 AIService.CompleteStream），
// 供终端助手 / 编辑器 / 文档助手复用。
import { Events } from '@wailsio/runtime'
import { AIService, EVENTS, type AIChatDelta } from './wails'
import {
  appendAiResult,
  bindAiResultCancel,
  failAiResult,
  finishAiResult,
  openAiStream,
} from './aiDialog'

export type AITask =
  | 'explain-command'
  | 'generate-command'
  | 'diagnose-command'
  | 'explain-code'
  | 'generate-code'
  | 'rewrite-code'
  | 'complete-code'
  | 'summarize'
  | 'rewrite-doc'
  | 'translate'
  | 'generate-doc'

interface PromptSpec {
  system: string
  build: (input: string, extra?: string) => string
}

const PROMPTS: Record<AITask, PromptSpec> = {
  'explain-command': {
    system: '你是 Linux / Shell 运维专家。请用简洁的中文解释用户给出的命令：它的作用、关键参数含义，以及需要注意的风险。',
    build: (cmd) => `请解释这条命令：\n\n${cmd}`,
  },
  'generate-command': {
    system:
      '你是 Linux Shell 专家。请把用户用自然语言描述的需求转成一条可直接执行的命令。只输出命令本身（可附一行简短注释），不要输出多余解释。',
    build: (desc) => `需求：${desc}`,
  },
  'diagnose-command': {
    system: '你是 Linux 运维专家。请根据命令和它的报错输出，用中文诊断问题原因，并给出可执行的修复建议。',
    build: (cmd, err) => `命令：\n${cmd}\n\n报错输出：\n${err || '（无输出）'}`,
  },
  'explain-code': {
    system: '你是资深程序员。请用简洁的中文解释这段代码的功能、关键逻辑和潜在问题。',
    build: (code, lang) => `语言：${lang || '未知'}\n\n代码：\n${code}`,
  },
  'generate-code': {
    system: '你是资深程序员。请根据描述生成代码，只输出代码（可带必要的简短说明），不要多余寒暄。',
    build: (desc, lang) => `语言：${lang || '按需选择'}\n\n需求：${desc}`,
  },
  'rewrite-code': {
    system: '你是资深程序员。请按用户要求优化 / 改写代码，只输出改写后的代码。',
    build: (code, req) => `代码：\n${code}\n\n改写要求：${req || '优化代码质量、可读性与健壮性'}`,
  },
  'complete-code': {
    system: '你是资深程序员。请补全 / 续写这段代码使其完整可用，只输出补全后的完整代码。',
    build: (code) => `请补全以下代码：\n\n${code}`,
  },
  summarize: {
    system: '你是编辑助手。请用简洁的中文总结以下文本的要点，分条列出。',
    build: (text) => text,
  },
  'rewrite-doc': {
    system: '你是编辑助手。请改写以下文本，使其更清晰、通顺、专业，保持原意。只输出改写后的文本。',
    build: (text, req) => (req ? `改写要求：${req}\n\n文本：\n${text}` : text),
  },
  translate: {
    system: '你是翻译。请把以下文本翻译成中文（若已是中文则翻译成英文），只输出译文。',
    build: (text) => text,
  },
  'generate-doc': {
    system: '你是写作助手。请根据描述生成内容，直接输出正文。',
    build: (desc) => `请写：${desc}`,
  },
}

// 调用后端一次性补全并返回文本。错误原样抛出，由调用方提示。
export async function runAI(task: AITask, input: string, extra?: string): Promise<string> {
  const spec = PROMPTS[task]
  return AIService.Complete(spec.system, spec.build(input, extra))
}

export interface AIStream {
  cancel: () => void
  // 成功（或手动停止）时解析为累积的完整文本；出错时 reject。
  done: Promise<string>
}

// 发起一次流式补全：每个增量通过 onDelta 回调，最终以 done 结束。
export function streamAI(
  task: AITask,
  input: string,
  extra: string | undefined,
  onDelta: (delta: string) => void,
): AIStream {
  const spec = PROMPTS[task]
  const requestId = 'ai-' + Math.random().toString(36).slice(2) + Date.now().toString(36)

  let acc = ''
  let settled = false
  let resolveFn!: (s: string) => void
  let rejectFn!: (e: Error) => void
  const done = new Promise<string>((resolve, reject) => {
    resolveFn = resolve
    rejectFn = reject
  })

  const un = Events.On(EVENTS.aiDelta, (evt: any) => {
    const d = evt?.data as AIChatDelta | undefined
    if (!d || d.requestId !== requestId) return
    if (d.delta) {
      acc += d.delta
      onDelta(d.delta)
    }
    if (d.done) {
      un()
      if (settled) return
      settled = true
      if (d.error && !/已停止/.test(d.error)) rejectFn(new Error(d.error))
      else resolveFn(acc)
    }
  })

  const cancel = () => {
    AIService.Cancel(requestId).catch(() => undefined)
  }

  AIService.CompleteStream(requestId, spec.system, spec.build(input, extra)).catch((e: any) => {
    un()
    if (!settled) {
      settled = true
      rejectFn(e instanceof Error ? e : new Error(e?.message ?? String(e)))
    }
  })

  return { cancel, done }
}

export interface StreamDialogOptions {
  title: string
  insertLabel?: string
  replaceLabel?: string
  onInsert?: (text: string) => void | Promise<void>
  onReplace?: (text: string) => void | Promise<void>
}

// 打开结果弹窗并流式填充（编辑器 / 文档助手用）：弹窗先出现，文本随增量增长，
// 生成期间「插入 / 替换」按钮禁用，完成后可一键应用到编辑器 / 文档。
export function streamToDialog(
  task: AITask,
  input: string,
  extra: string | undefined,
  opts: StreamDialogOptions,
): AIStream {
  openAiStream({
    title: opts.title,
    insertLabel: opts.insertLabel,
    replaceLabel: opts.replaceLabel,
    onInsert: opts.onInsert,
    onReplace: opts.onReplace,
  })
  const stream = streamAI(task, input, extra, (delta) => appendAiResult(delta))
  bindAiResultCancel(stream.cancel)
  stream.done
    .then(() => finishAiResult())
    .catch((e: Error) => failAiResult(e?.message || String(e)))
  return stream
}
