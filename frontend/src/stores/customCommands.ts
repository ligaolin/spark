import { defineStore } from 'pinia'
import { CustomCommandService, makeCustomCommand } from '../utils/wails'
import type { CustomCommand } from '../utils/wails'

// 自定义命令：存数据库（SQLite），不再用 localStorage
export const useCustomCommandsStore = defineStore('customCommands', {
  state: () => ({
    items: [] as CustomCommand[],
    loading: false,
  }),

  actions: {
    async load() {
      this.loading = true
      try {
        this.items = (await CustomCommandService.List()) ?? []
      } finally {
        this.loading = false
      }
    },

    async add(name: string, command: string) {
      await CustomCommandService.Create(makeCustomCommand({ name, command }))
      await this.load()
    },

    async update(id: number, name: string, command: string) {
      await CustomCommandService.Update(makeCustomCommand({ id, name, command }))
      await this.load()
    },

    async remove(id: number) {
      await CustomCommandService.Delete(id)
      await this.load()
    },
  },
})
