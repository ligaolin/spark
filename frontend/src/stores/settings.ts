import { defineStore } from 'pinia'
import { SettingsService } from '../utils/wails'
import { applyTheme, cacheTheme, type Theme } from '../utils/theme'

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
    // 网络面板自动刷新间隔（秒）
    networkRefreshInterval(state): number {
      const v = parseInt(state.values['network.refresh'] ?? '', 10)
      return Number.isFinite(v) && v > 0 ? v : 5
    },
    // 终端字号
    terminalFontSize(state): number {
      const v = parseInt(state.values['terminal.fontSize'] ?? '', 10)
      return Number.isFinite(v) && v >= 8 && v <= 32 ? v : 13
    },
    // 点窗口关闭按钮的行为：minimize=缩小到托盘，exit=直接退出
    windowCloseAction(state): 'minimize' | 'exit' {
      return state.values['window.closeAction'] === 'exit' ? 'exit' : 'minimize'
    },
    // 编辑器是否自动换行（长行折行显示）
    editorWordWrap(state): boolean {
      return state.values['editor.wordWrap'] !== '0'
    },
    // 编辑器双击选中单词时的分隔符（留空 = 默认：字母数字为单词，其余符号都截断）
    editorWordSeparators(state): string {
      return state.values['editor.wordSeparators'] ?? ''
    },
    // 编辑器切换标签时，是否在左侧文件树自动展开并选中、滚动到对应文件（默认开启）
    editorTreeFollow(state): boolean {
      return state.values['editor.treeFollow'] !== '0'
    },
    // 明暗主题（默认暗色，保持现有行为）
    theme(state): Theme {
      return state.values['app.theme'] === 'light' ? 'light' : 'dark'
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

    // 切换明暗主题：持久化到数据库设置，并同步应用 + 写启动缓存
    async setTheme(theme: Theme) {
      await this.set('app.theme', theme)
      applyTheme(theme)
      cacheTheme(theme)
    },
  },
})
