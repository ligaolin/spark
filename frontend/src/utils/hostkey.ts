// 主机密钥相关工具：解析后端返回的标记化错误，弹出信任确认。
import { HostKeyService } from './wails'
import { showConfirmDialog, showAlertDialog } from './dialog'
import type { ConnectOptions } from './wails'

export interface HostKeyIssue {
  type: 'unknown' | 'mismatch' | 'revoked'
  fingerprint: string
  old?: string
}

// 解析形如 "HOSTKEY_UNKNOWN|SHA256:xxx" 的错误消息
export function parseHostKeyError(msg: string): HostKeyIssue | null {
  if (!msg) return null
  if (msg.includes('HOSTKEY_UNKNOWN|')) {
    return { type: 'unknown', fingerprint: extract(msg, 'HOSTKEY_UNKNOWN|') }
  }
  if (msg.includes('HOSTKEY_MISMATCH|')) {
    const rest = extract(msg, 'HOSTKEY_MISMATCH|')
    const [fp, old] = rest.split('|')
    return { type: 'mismatch', fingerprint: fp || '', old: old || '' }
  }
  if (msg.includes('HOSTKEY_REVOKED|')) {
    return { type: 'revoked', fingerprint: extract(msg, 'HOSTKEY_REVOKED|') }
  }
  return null
}

function extract(msg: string, marker: string): string {
  const idx = msg.indexOf(marker)
  if (idx < 0) return ''
  return msg.slice(idx + marker.length).trim()
}

/**
 * 处理连接失败中的主机密钥问题：弹窗询问用户是否信任，
 * 信任则调用 AcceptHostKey 保存后返回 true（调用方应重试连接）。
 */
export async function resolveHostKeyIssue(
  err: any,
  opts: ConnectOptions,
): Promise<boolean> {
  const issue = parseHostKeyError(err?.message || String(err))
  if (!issue) return false

  let html: string
  let title: string
  if (issue.type === 'unknown') {
    title = '未知的主机密钥'
    html = `
      <div style="font-size:13px;line-height:1.8">
        <p>无法确认 <b>${escapeHtml(opts.host)}</b> 的身份，该主机的密钥从未见过。</p>
        <p>主机密钥 SHA256 指纹：<code>${escapeHtml(issue.fingerprint)}</code></p>
        <p style="color:#e6a23c">如果这是第一次连接，通常可以信任；请确认没有中间人攻击风险。</p>
      </div>`
  } else if (issue.type === 'mismatch') {
    title = '主机密钥不匹配！'
    html = `
      <div style="font-size:13px;line-height:1.8">
        <p><b>${escapeHtml(opts.host)}</b> 的密钥与已保存的不一致，可能存在中间人攻击。</p>
        <p>新指纹：<code>${escapeHtml(issue.fingerprint)}</code></p>
        <p>旧指纹：<code>${escapeHtml(issue.old || '')}</code></p>
        <p style="color:#f56c6c">确认主机信息无误后，可以更新保存的密钥并继续。</p>
      </div>`
  } else {
    title = '主机密钥已被吊销'
    html = `
      <div style="font-size:13px;line-height:1.8">
        <p><b>${escapeHtml(opts.host)}</b> 的密钥在 known_hosts 中被标记为已吊销。</p>
        <p>指纹：<code>${escapeHtml(issue.fingerprint)}</code></p>
        <p style="color:#f56c6c">建议停止连接并核实主机身份。</p>
      </div>`
  }

  const ok = await showConfirmDialog(
    title,
    html,
    issue.type === 'mismatch',
    issue.type === 'revoked' ? '仍然信任并继续（不安全）' : '信任并继续',
  )
  if (!ok) return false

  try {
    // 信任/替换 known_hosts 条目（revoked 条目同样会被替换掉）
    await HostKeyService.AcceptHostKey(opts)
    return true
  } catch (e: any) {
    await showAlertDialog('错误', `保存主机密钥失败：${e?.message || e}`)
    return false
  }
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}
