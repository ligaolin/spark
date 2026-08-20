import { defineStore } from 'pinia'
import { useSettingsStore } from './settings'

export interface ShortcutAction {
  id: string
  label: string
  hint: string
  defaultKey: string
  key: string // 当前绑定（如 "Ctrl+T"）
}

// 快捷键默认值；用户修改后覆盖保存到数据库（shortcut.<id>）
export const SHORTCUT_DEFAULTS: ShortcutAction[] = [
  { id: 'nav.connections', label: '打开连接管理', hint: '切换到连接管理页', defaultKey: 'Ctrl+1', key: 'Ctrl+1' },
  { id: 'nav.terminal', label: '打开 SSH 终端', hint: '切换到 SSH 终端页', defaultKey: 'Ctrl+2', key: 'Ctrl+2' },
  { id: 'nav.sftp', label: '打开 SFTP 文件', hint: '切换到 SFTP 文件页', defaultKey: 'Ctrl+3', key: 'Ctrl+3' },
  { id: 'nav.ftp', label: '打开 FTP 文件', hint: '切换到 FTP 文件页', defaultKey: 'Ctrl+4', key: 'Ctrl+4' },
  { id: 'terminal.new', label: '新建 SSH 会话', hint: '跳转终端页并打开新建会话对话框', defaultKey: 'Ctrl+T', key: 'Ctrl+T' },
  { id: 'terminal.close', label: '关闭当前终端标签', hint: '关闭当前活动标签', defaultKey: 'Ctrl+W', key: 'Ctrl+W' },
  { id: 'panel.toggle', label: '开关终端信息面板', hint: '展开/收起右侧信息面板', defaultKey: 'Ctrl+B', key: 'Ctrl+B' },
  { id: 'devtools.toggle', label: '打开调试工具', hint: '打开浏览器调试窗口（DevTools）', defaultKey: 'F12', key: 'F12' },
]

export const useShortcutsStore = defineStore('shortcuts', {
  state: () => ({
    items: SHORTCUT_DEFAULTS.map((s) => ({ ...s })),
  }),

  actions: {
    // 从数据库加载覆盖值
    async load() {
      const settings = useSettingsStore()
      await settings.load()
      for (const item of this.items) {
        const saved = settings.values[`shortcut.${item.id}`]
        if (saved) item.key = saved
      }
    },

    // 保存某个快捷键的绑定（校验冲突与格式）
    async bind(id: string, combo: string): Promise<string | null> {
      const normalized = normalizeCombo(combo)
      if (!normalized) return '无效的快捷键'
      const target = this.items.find((i) => i.id === id)
      if (!target) return '未知的快捷键'
      const conflict = this.items.find(
        (i) => i.id !== id && i.key.toLowerCase() === normalized.toLowerCase(),
      )
      if (conflict) return `与「${conflict.label}」冲突`
      target.key = normalized
      const settings = useSettingsStore()
      await settings.set(`shortcut.${id}`, normalized)
      return null
    },

    async reset(id: string) {
      const target = this.items.find((i) => i.id === id)
      if (!target) return
      target.key = target.defaultKey
      const settings = useSettingsStore()
      await settings.set(`shortcut.${id}`, target.defaultKey)
    },

    async resetAll() {
      const settings = useSettingsStore()
      for (const item of this.items) {
        item.key = item.defaultKey
        await settings.set(`shortcut.${item.id}`, item.defaultKey)
      }
    },
  },
})

const KEY_NAMES: Record<string, string> = {
  ' ': 'space',
  ArrowUp: 'up',
  ArrowDown: 'down',
  ArrowLeft: 'left',
  ArrowRight: 'right',
  Enter: 'enter',
  Escape: 'esc',
  Tab: 'tab',
  Backspace: 'backspace',
  Delete: 'delete',
  Home: 'home',
  End: 'end',
  PageUp: 'pageup',
  PageDown: 'pagedown',
  Insert: 'insert',
  '+': 'plus',
}

// 把 KeyboardEvent 转成规范组合（如 "ctrl+shift+t"）
export function eventToCombo(e: KeyboardEvent): string {
  const parts: string[] = []
  if (e.ctrlKey || e.metaKey) parts.push('ctrl')
  if (e.altKey) parts.push('alt')
  if (e.shiftKey) parts.push('shift')
  let key = e.key
  if (KEY_NAMES[key] !== undefined) key = KEY_NAMES[key]
  else key = key.toLowerCase()
  if (key === 'control' || key === 'alt' || key === 'shift' || key === 'meta') {
    return '' // 单独按修饰键不算
  }
  return parts.length ? `${parts.join('+')}+${key}` : key
}

// 规范化并转为展示格式（如 "Ctrl+Shift+T"）
export function normalizeCombo(combo: string): string {
  const parts = combo
    .split('+')
    .map((p) => p.trim().toLowerCase())
    .filter(Boolean)
  if (parts.length === 0) return ''
  return parts
    .map((p) => (p === 'ctrl' || p === 'alt' || p === 'shift' ? p[0].toUpperCase() + p.slice(1) : p.toUpperCase()))
    .join('+')
}
