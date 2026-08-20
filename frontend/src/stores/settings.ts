import { defineStore } from 'pinia'
import { SettingsService } from '../utils/wails'

// 全局配置（存数据库 settings 表，key-value，可随时扩展）
export const useSettingsStore = defineStore('settings', {
  state: () => ({
    values: {} as Record<string, string>,
    loaded: false,
  }),

  getters: {
    // 保活间隔（秒，0=关闭）
    keepaliveInterval(state): number {
      const v = parseInt(state.values['keepalive.interval'] ?? '', 10)
      return Number.isFinite(v) ? v : 20
    },
    // 进程管理自动刷新间隔（秒）
    processRefreshInterval(state): number {
      const v = parseInt(state.values['process.refresh'] ?? '', 10)
      return Number.isFinite(v) && v > 0 ? v : 5
    },
    // 终端字号
    terminalFontSize(state): number {
      const v = parseInt(state.values['terminal.fontSize'] ?? '', 10)
      return Number.isFinite(v) && v >= 8 && v <= 32 ? v : 13
    },
  },

  actions: {
    async load() {
      try {
        this.values = ((await SettingsService.GetAll()) ?? {}) as Record<string, string>
      } catch {
        this.values = {}
      }
      this.loaded = true
    },

    async set(key: string, value: string) {
      await SettingsService.Set(key, value)
      this.values[key] = value
    },
  },
})
