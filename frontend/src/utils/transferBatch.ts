// 批量传输辅助：把一批文件的上传 / 下载 / 复制聚合成一条进度任务，
// 结束后只在发起页面弹一次提示（scope 隔离）。
import { useTransfersStore, type TransferKind, type TransferOp } from '../stores/transfers'
import { notifyScope } from './transferNotify'

export interface BatchTransferItem {
  // 展示名（同时用于把后端进度事件归到本批次）
  name: string
  // 实际要执行的操作
  run: () => Promise<void>
}

export interface BatchTransferOptions {
  kind: TransferKind
  scope: string
  sessionId: string
  op: TransferOp
  label: string
  items: BatchTransferItem[]
  success?: string
}

const OP_LABEL: Record<string, string> = {
  upload: '上传',
  download: '下载',
  delete: '删除',
  copy: '复制',
}

export async function runBatchTransfer(opts: BatchTransferOptions): Promise<{ ok: number; failed: string[] }> {
  const transfers = useTransfersStore()
  const key = transfers.start({
    kind: opts.kind,
    scope: opts.scope,
    sessionId: opts.sessionId,
    op: opts.op,
    label: opts.label,
    files: opts.items.map((it) => it.name),
  })

  let ok = 0
  const failed: string[] = []
  for (const it of opts.items) {
    transfers.expectName(key, opts.sessionId, opts.op, it.name)
    try {
      await it.run()
      ok++
      transfers.tick(key)
    } catch (e: any) {
      failed.push(it.name)
      transfers.tick(key, { error: `${it.name}: ${e?.message || e}` })
    }
  }

  const opLabel = OP_LABEL[opts.op] ?? opts.op
  if (failed.length === 0) {
    transfers.finish(key)
    notifyScope(opts.scope, 'success', opts.success ?? `${opLabel}完成（${ok} 项）`)
  } else {
    const msg = `${opLabel}完成：成功 ${ok} 项，失败 ${failed.length} 项（${failed.slice(0, 3).join('、')}${failed.length > 3 ? '…' : ''}）`
    transfers.fail(key, msg)
    notifyScope(opts.scope, 'error', msg)
  }
  return { ok, failed }
}
