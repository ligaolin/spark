import { defineStore } from 'pinia'
import { LocalTerminalService } from '../utils/wails'

export interface LocalTerminalTab {
  key: string // 自增唯一 key（前端标识）
  sessionId: string
  title: string
  status: 'starting' | 'running' | 'closed' | 'error'
  error?: string
  exitCode?: number
  shell: string // 创建时使用的 shell（'' = 平台默认；'powershell' 等）
  cwd: string // 终端起始目录（'' = 用户主目录）
}

let tabSeq = 1

export const useLocalTerminalStore = defineStore('localTerminal', {
  state: () => ({
    tabs: [] as LocalTerminalTab[],
    activeKey: '',
  }),

  getters: {
    activeTab(state): LocalTerminalTab | undefined {
      return state.tabs.find((t) => t.key === state.activeKey)
    },
  },

  actions: {
    addTab(shell = '', cwd = ''): LocalTerminalTab {
      const tab: LocalTerminalTab = {
        key: `loc-${tabSeq++}`,
        sessionId: '',
        title: '本地终端',
        status: 'starting',
        shell,
        cwd,
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
        LocalTerminalService.Disconnect(tab.sessionId).catch(() => undefined)
      }
      this.tabs.splice(idx, 1)
      if (this.activeKey === key) {
        this.activeKey = this.tabs.length ? this.tabs[Math.max(0, idx - 1)].key : ''
      }
    },

    setActive(key: string) {
      this.activeKey = key
    },

    markStarting(key: string) {
      const tab = this.tabs.find((t) => t.key === key)
      if (tab) {
        tab.status = 'starting'
        tab.error = undefined
        tab.exitCode = undefined
      }
    },

    markRunning(key: string, sessionId: string) {
      const tab = this.tabs.find((t) => t.key === key)
      if (tab) {
        tab.sessionId = sessionId
        tab.status = 'running'
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
