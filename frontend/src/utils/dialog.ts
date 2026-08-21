// 页面内弹窗服务：代替编程式 ElMessageBox（prompt / confirm / alert），
// 用页面中的 el-dialog 渲染，样式正常加载、支持多字段。
import { reactive } from 'vue'

export interface DialogField {
  key: string
  label: string
  placeholder?: string
  initial?: string
  type?: 'text' | 'password' | 'select'
  options?: { label: string; value: string }[] // type 为 select 时的选项
  optional?: boolean // 为 true 时允许留空
}

interface InputState {
  visible: boolean
  title: string
  fields: DialogField[]
  resolve: ((v: Record<string, string> | null) => void) | null
}

interface ConfirmState {
  visible: boolean
  title: string
  message: string
  danger: boolean
  confirmText: string
  showCancel: boolean
  resolve: ((v: boolean) => void) | null
}

export const inputState = reactive<InputState>({
  visible: false,
  title: '',
  fields: [],
  resolve: null,
})

export const confirmState = reactive<ConfirmState>({
  visible: false,
  title: '',
  message: '',
  danger: false,
  confirmText: '确定',
  showCancel: true,
  resolve: null,
})

// 输入弹窗：确认返回各字段值，取消返回 null
export function showInputDialog(
  title: string,
  fields: DialogField[],
): Promise<Record<string, string> | null> {
  inputState.title = title
  inputState.fields = fields
  inputState.visible = true
  return new Promise((resolve) => {
    inputState.resolve = resolve
  })
}

// 确认弹窗：确定返回 true，取消返回 false
export function showConfirmDialog(
  title: string,
  message: string,
  danger = false,
  confirmText = '确定',
): Promise<boolean> {
  confirmState.title = title
  confirmState.message = message
  confirmState.danger = danger
  confirmState.confirmText = confirmText
  confirmState.showCancel = true
  confirmState.visible = true
  return new Promise((resolve) => {
    confirmState.resolve = resolve
  })
}

// 提示弹窗：只有"确定"按钮
export function showAlertDialog(title: string, message: string): Promise<void> {
  confirmState.title = title
  confirmState.message = message
  confirmState.danger = false
  confirmState.confirmText = '确定'
  confirmState.showCancel = false
  confirmState.visible = true
  return new Promise<void>((resolve) => {
    confirmState.resolve = () => resolve()
  })
}
