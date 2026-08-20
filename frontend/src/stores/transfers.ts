import { defineStore } from 'pinia'
import { EVENTS } from '../utils/wails'
import { Events } from '@wailsio/runtime'
import type { TransferProgress } from '../types'

export interface TransferItem extends TransferProgress {
  key: string
  status: 'running' | 'done' | 'error'
  error?: string
  // 每次进度事件的快照（用于百分比）
  percent: number
}

let transferSeq = 1

export const useTransfersStore = defineStore('transfers', {
  state: () => ({
    items: [] as TransferItem[],
    visible: true,
    _bound: false,
  }),

  actions: {
    // 绑定一次全局进度事件监听
    bind() {
      if (this._bound) return
      this._bound = true
      Events.On(EVENTS.transferProgress, (evt: any) => {
        const p: TransferProgress = evt.data
        if (!p) return
        let item = this.items.find(
          (i) => i.sessionId === p.sessionId && i.op === p.op && i.name === p.name,
        )
        if (!item) {
          item = {
            key: `tf-${transferSeq++}`,
            sessionId: p.sessionId,
            op: p.op,
            name: p.name,
            done: p.done,
            total: p.total,
            status: 'running',
            percent: p.total > 0 ? Math.min(100, Math.round((p.done / p.total) * 100)) : 0,
          }
          this.items.push(item)
        } else {
          item.done = p.done
          item.total = p.total
          item.percent = p.total > 0 ? Math.min(100, Math.round((p.done / p.total) * 100)) : 0
          if (item.status === 'done') item.status = 'running'
        }
      })
    },

    complete(sessionId: string, op: string, name: string) {
      const item = this.items.find((i) => i.sessionId === sessionId && i.op === op && i.name === name)
      if (item) {
        item.status = 'done'
        item.percent = 100
        item.done = item.total || item.done
        // 完成 3 秒后自动移除
        setTimeout(() => this.remove(item.key), 3000)
      }
    },

    fail(sessionId: string, op: string, name: string, error: string) {
      const item = this.items.find((i) => i.sessionId === sessionId && i.op === op && i.name === name)
      if (item) {
        item.status = 'error'
        item.error = error
      }
    },

    remove(key: string) {
      const idx = this.items.findIndex((i) => i.key === key)
      if (idx >= 0) this.items.splice(idx, 1)
    },

    clear() {
      this.items = []
    },
  },
})
