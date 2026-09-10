// AI 结果弹窗的全局状态（配合 components/AiResultDialog.vue）。
// 支持两种模式：一次性结果（showAiResult）与流式结果（openAiStream + append）。
import { reactive } from 'vue'

export interface AiResultOptions {
  title: string
  text?: string
  // 可选动作按钮：展示为「插入…」「替换…」
  insertLabel?: string
  replaceLabel?: string
  onInsert?: (text: string) => void | Promise<void>
  onReplace?: (text: string) => void | Promise<void>
}

export interface AiStreamOptions {
  title: string
  insertLabel?: string
  replaceLabel?: string
  onInsert?: (text: string) => void | Promise<void>
  onReplace?: (text: string) => void | Promise<void>
}

interface AiResultState {
  visible: boolean
  title: string
  text: string
  loading: boolean // 动作按钮（插入/替换）执行中
  streaming: boolean // 正在流式生成中
  error: string
  insertLabel: string
  replaceLabel: string
  onInsert: ((t: string) => void | Promise<void>) | null
  onReplace: ((t: string) => void | Promise<void>) | null
  cancel: (() => void) | null
}

export const aiResultState = reactive<AiResultState>({
  visible: false,
  title: '',
  text: '',
  loading: false,
  streaming: false,
  error: '',
  insertLabel: '',
  replaceLabel: '',
  onInsert: null,
  onReplace: null,
  cancel: null,
})

// 展示一次性结果（非流式）。
export function showAiResult(opts: AiResultOptions) {
  aiResultState.title = opts.title
  aiResultState.text = opts.text ?? ''
  aiResultState.error = ''
  aiResultState.insertLabel = opts.insertLabel ?? ''
  aiResultState.replaceLabel = opts.replaceLabel ?? ''
  aiResultState.onInsert = opts.onInsert ?? null
  aiResultState.onReplace = opts.onReplace ?? null
  aiResultState.loading = false
  aiResultState.streaming = false
  aiResultState.cancel = null
  aiResultState.visible = true
}

// 打开流式结果弹窗：先显示空结果与「生成中…」，随后用 appendAiResult 逐步填充。
export function openAiStream(opts: AiStreamOptions) {
  aiResultState.title = opts.title
  aiResultState.text = ''
  aiResultState.error = ''
  aiResultState.insertLabel = opts.insertLabel ?? ''
  aiResultState.replaceLabel = opts.replaceLabel ?? ''
  aiResultState.onInsert = opts.onInsert ?? null
  aiResultState.onReplace = opts.onReplace ?? null
  aiResultState.loading = false
  aiResultState.streaming = true
  aiResultState.cancel = null
  aiResultState.visible = true
}

export function appendAiResult(delta: string) {
  aiResultState.text += delta
}

export function bindAiResultCancel(cancel: () => void) {
  aiResultState.cancel = cancel
}

export function finishAiResult() {
  aiResultState.streaming = false
  aiResultState.cancel = null
}

export function failAiResult(error: string) {
  aiResultState.streaming = false
  aiResultState.error = error
  aiResultState.cancel = null
}

export function closeAiResult() {
  aiResultState.cancel?.()
  aiResultState.visible = false
  aiResultState.streaming = false
  aiResultState.error = ''
  aiResultState.cancel = null
}
