import { defineStore } from 'pinia'
import { ConnService } from '../utils/wails'
import type { SavedConnection, ConnectionGroup } from '../utils/wails'

export const useConnectionsStore = defineStore('connections', {
  state: () => ({
    list: [] as SavedConnection[],
    groups: [] as ConnectionGroup[],
    loading: false,
  }),

  getters: {
    groupNames: (state) => state.groups.map((g) => g.name),
  },

  actions: {
    async load() {
      this.loading = true
      try {
        this.list = (await ConnService.List()) ?? []
      } finally {
        this.loading = false
      }
      await this.loadGroups()
    },

    async loadGroups() {
      this.groups = (await ConnService.ListGroups()) ?? []
    },

    async create(conn: SavedConnection): Promise<SavedConnection> {
      const saved = await ConnService.Create(conn)
      await this.load()
      return saved
    },

    async update(conn: SavedConnection): Promise<SavedConnection> {
      const saved = await ConnService.Update(conn)
      await this.load()
      return saved
    },

    async remove(id: number) {
      await ConnService.Delete(id)
      await this.load()
    },

    async setGroup(id: number, group: string) {
      await ConnService.SetGroup(id, group)
      await this.load()
    },

    async createGroup(name: string) {
      await ConnService.CreateGroup(name)
      await this.loadGroups()
    },

    async renameGroup(oldName: string, newName: string) {
      await ConnService.RenameGroup(oldName, newName)
      await this.load()
    },

    async deleteGroup(name: string) {
      await ConnService.DeleteGroup(name)
      await this.load()
    },
  },
})
