import { defineStore } from 'pinia'
import { FTPFileService } from '../utils/wails'
import type { ConnectOptions } from '../utils/wails'

export interface FtpTab {
  key: string
  sessionId: string
  title: string
  status: 'connecting' | 'connected' | 'closed' | 'error'
  error?: string
  opts: ConnectOptions
  connId?: number
  exitCode?: number
}

let tabSeq = 1

export const useFtpStore = defineStore('ftp', {
  state: () => ({
    tabs: [] as FtpTab[],
    activeKey: '',
  }),

  getters: {
    activeTab(state): FtpTab | undefined {
      return state.tabs.find((t) => t.key === state.activeKey)
    },
  },

  actions: {
    addTab(opts: ConnectOptions, connId?: number, connName?: string): FtpTab {
      const tab: FtpTab = {
        key: `ftp-${tabSeq++}`,
        sessionId: '',
        title: connName || (opts.host ? `${opts.username}@${opts.host}:${opts.port || 21}` : '新连接'),
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
        FTPFileService.Disconnect(tab.sessionId).catch(() => undefined)
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