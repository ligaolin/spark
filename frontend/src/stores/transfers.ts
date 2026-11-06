import { defineStore } from 'pinia'
import { EVENTS } from '../utils/wails'
import { Events } from '@wailsio/runtime'
import type { TransferProgress } from '../types'

export type TransferKind = 'sftp' | 'ftp' | 'editor' | 'local'
export type TransferOp = 'upload' | 'download' | 'delete' | 'copy'

// 一次批量传输（上传一批文件 / 下载一批文件 / 批量删除）对应界面上的
// 一条进度条，不再按文件逐条刷屏。
export interface TransferTask {
  key: string
  kind: TransferKind
  // 来源页面标识：SFTP 终端面板 / FTP 页 / 独立 SFTP 页各自只看自己的任务
  scope: string
  sessionId: string
  op: TransferOp
  label: string
  status: 'running' | 'done' | 'error'
  error?: string
  fileTotal: number
  fileDone: number
  fileFailed: number
  totalBytes: number
  doneBytes: number
  percent: number
  startedAt: number
}

export interface StartTaskOptions {
  kind: TransferKind
  scope: string
  sessionId: string
  op: TransferOp
  label: string
  files?: string[]
  fileTotal?: number
  totalBytes?: number
}

let taskSeq = 1
// 文件级进度事件 → 任务：后端事件只带 sessionId/op/name，这里按前端登记的名字
// 归属到对应批次；未登记的名字（目录内部的子文件）回落到该会话最近的任务。
const expected = new Map<string, string>()
// 每个任务内按文件名累计字节数，用于合并成批次总进度。
const byteMap = new Map<string, Map<string, { done: number; total: number }>>()

function fileSig(sessionId: string, op: string, name: string): string {
  return `${sessionId}\u0000${op}\u0000${name}`
}

export const useTransfersStore = defineStore('transfers', {
  state: () => ({
    tasks: [] as TransferTask[],
    _bound: false,
  }),

  actions: {
    bind() {
      if (this._bound) return
      this._bound = true
      Events.On(EVENTS.transferProgress, (evt: any) => {
        const p = evt.data as TransferProgress
        if (!p) return
        const taskKey =
          expected.get(fileSig(p.sessionId, p.op, p.name)) ??
          this.latestRunning(p.sessionId, p.op)
        if (!taskKey) return
        const task = this.tasks.find((t) => t.key === taskKey)
        if (!task) return

        let perTask = byteMap.get(taskKey)
        if (!perTask) {
          perTask = new Map()
          byteMap.set(taskKey, perTask)
        }
        perTask.set(p.name, { done: p.done, total: p.total })
        let done = 0
        let total = 0
        for (const b of perTask.values()) {
          done += b.done
          total += b.total
        }
        task.doneBytes = done
        task.totalBytes = Math.max(task.totalBytes, total)
        task.percent = this.calcPercent(task)
        if (task.status === 'done') task.status = 'running'
      })
    },

    latestRunning(sessionId: string, op: string): string | undefined {
      for (let i = this.tasks.length - 1; i >= 0; i--) {
        const t = this.tasks[i]
        if (t.sessionId === sessionId && t.op === op && t.status === 'running') return t.key
      }
      return undefined
    },

    calcPercent(t: TransferTask): number {
      if (t.totalBytes > 0) {
        return Math.min(100, Math.round((t.doneBytes / t.totalBytes) * 100))
      }
      if (t.fileTotal > 0) {
        return Math.min(100, Math.round((t.fileDone / t.fileTotal) * 100))
      }
      return t.status === 'done' ? 100 : 0
    },

    start(opts: StartTaskOptions): string {
      const key = `tf-${taskSeq++}`
      const task: TransferTask = {
        key,
        kind: opts.kind,
        scope: opts.scope,
        sessionId: opts.sessionId,
        op: opts.op,
        label: opts.label,
        status: 'running',
        fileTotal: opts.fileTotal ?? opts.files?.length ?? 0,
        fileDone: 0,
        fileFailed: 0,
        totalBytes: opts.totalBytes ?? 0,
        doneBytes: 0,
        percent: 0,
        startedAt: Date.now(),
      }
      this.tasks.push(task)
      if (task.fileTotal === 0 && opts.files?.length) task.fileTotal = opts.files.length
      for (const name of opts.files ?? []) {
        expected.set(fileSig(opts.sessionId, opts.op, name), key)
      }
      // 只保留最近 40 条，避免长时间运行后无限增长
      if (this.tasks.length > 40) {
        const removed = this.tasks.splice(0, this.tasks.length - 40)
        for (const r of removed) byteMap.delete(r.key)
      }
      return key
    },

    // 登记批次内的一个文件名（上传前调用，保证进度事件能归到该批次）
    expectName(key: string, sessionId: string, op: string, name: string) {
      const task = this.tasks.find((t) => t.key === key)
      if (!task) return
      expected.set(fileSig(sessionId, op, name), key)
      if (task.fileTotal === 0) task.fileTotal = 1
      void key
    },

    // 单个条目完成（成功 / 失败都计入进度；失败会记录下来）
    tick(key: string, opts: { error?: string } = {}) {
      const task = this.tasks.find((t) => t.key === key)
      if (!task) return
      task.fileDone++
      if (opts.error) {
        task.fileFailed++
        if (!task.error) task.error = opts.error
      }
      task.percent = this.calcPercent(task)
    },

    finish(key: string) {
      const task = this.tasks.find((t) => t.key === key)
      if (!task) return
      task.status = task.fileFailed > 0 ? 'error' : 'done'
      task.percent = 100
      task.fileDone = Math.max(task.fileDone, task.fileTotal)
      task.doneBytes = task.totalBytes || task.doneBytes
      const k = task.key
      setTimeout(() => this.remove(k), 2500)
    },

    fail(key: string, error: string) {
      const task = this.tasks.find((t) => t.key === key)
      if (!task) return
      task.status = 'error'
      task.error = error
      task.percent = 100
    },

    remove(key: string) {
      const idx = this.tasks.findIndex((t) => t.key === key)
      if (idx >= 0) this.tasks.splice(idx, 1)
      byteMap.delete(key)
    },

    clear(scope?: string) {
      if (!scope) {
        this.tasks = []
        byteMap.clear()
        expected.clear()
        return
      }
      this.tasks = this.tasks.filter((t) => t.scope !== scope)
    },
  },
})
