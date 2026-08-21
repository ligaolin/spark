import { defineStore } from 'pinia'
import { TerminalService } from '../utils/wails'
import type { ConnectOptions } from '../utils/wails'

export interface TerminalTab {
  key: string // 自增唯一 key（前端标识）
  sessionId: string
  title: string
  status: 'connecting' | 'connected' | 'closed' | 'error'
  error?: string
  opts: ConnectOptions
  // 来源已保存连接的 id（从连接管理打开时有值）：用于 SFTP 面板的目录收藏
  connId?: number
  exitCode?: number
}

let tabSeq = 1

export const useTerminalStore = defineStore('terminal', {
  state: () => ({
    tabs: [] as TerminalTab[],
    activeKey: '',
  }),

  getters: {
    activeTab(state): TerminalTab | undefined {
      return state.tabs.find((t) => t.key === state.activeKey)
    },
  },

  actions: {
    addTab(opts: ConnectOptions, connId?: number): TerminalTab {
      const tab: TerminalTab = {
        key: `tab-${tabSeq++}`,
        sessionId: '',
        title: opts.host ? `${opts.username}@${opts.host}:${opts.port || 22}` : '新会话',
        status: 'connecting',
        opts: { ...opts },
        ...(connId ? { connId } : {}),
      }
      this.tabs.push(tab)
      this.activeKey = tab.key
      return tab
    },

    removeTab(key: string) {
      const idx = this.tabs.findIndex((t) => t.key === key)
      if (idx < 0) return
      const tab = this.tabs[idx]
      if (tab.sessionId) {
        TerminalService.Disconnect(tab.sessionId).catch(() => undefined)
      }
      this.tabs.splice(idx, 1)
      if (this.activeKey === key) {
        this.activeKey = this.tabs.length ? this.tabs[Math.max(0, idx - 1)].key : ''
      }
    },

    setActive(key: string) {
      this.activeKey = key
    },

    markConnecting(key: string) {
      const tab = this.tabs.find((t) => t.key === key)
      if (tab) {
        tab.status = 'connecting'
        tab.error = undefined
        tab.exitCode = undefined
      }
    },

    markConnected(key: string, sessionId: string) {
      const tab = this.tabs.find((t) => t.key === key)
      if (tab) {
        tab.sessionId = sessionId
        tab.status = 'connected'
        tab.error = undefined
      }
    },

    markClosed(key: string, code: number, error?: string) {
      const tab = this.tabs.find((t) => t.key === key)
      if (tab && tab.status !== 'closed') {
        tab.status = 'closed'
        tab.exitCode = code
        tab.error = error
      }
    },

    markError(key: string, error: string) {
      const tab = this.tabs.find((t) => t.key === key)
      if (tab) {
        tab.status = 'error'
        tab.error = error
      }
    },
  },
})
