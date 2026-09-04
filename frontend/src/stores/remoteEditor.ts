// 远程编辑器（FTP / SFTP）共享状态。
// 编辑器板块是应用级功能：SFTP / FTP 文件页右键把目录/文件交给这里，
// 由独立的「远程编辑器」页面渲染。多个板块可同时存在、可关闭（释放资源）。
import { reactive } from 'vue'
import type { FileBackend } from '../utils/fileBackend'
import { parentDir } from '../utils/fileBackend'

export interface RemotePanelInfo {
  id: number
  title: string
  rootPath: string
  // 每个板块记录打开时的后端（SFTP/FTP 会话绑定），互不串扰
  backend: FileBackend
  // 来源作用域：从 SSH 终端 SFTP 面板打开的板块记下对应的 SSH 标签 key，
  // 只有该标签真正被关闭 / 会话断开时才随之关闭；切换标签不再影响它。
  scope?: string
}

interface RemoteEditorState {
  panels: RemotePanelInfo[]
  activeId: number | null
  // 板块挂载后需要打开的文件（双击文件 → 新板块 → 挂载完成后打开）
  pendingOpen: { path: string } | null
}

export const remoteEditor = reactive<RemoteEditorState>({
  panels: [],
  activeId: null,
  pendingOpen: null,
})

let seq = 0

function basename(p: string): string {
  const parts = p.split(/[\\/]/)
  return parts[parts.length - 1] || p
}

// 同路径去重限定在同一作用域内：不同 SSH 标签（不同服务器）打开同一目录
// 应各自成板块，不能被合并到第一个标签的板块里。
function samePanel(p: RemotePanelInfo, path: string, scope?: string): boolean {
  return p.rootPath === path && p.scope === scope
}

// 右键目录：把指定目录加载到编辑器板块（已存在则激活）
export function openDirInEditor(backend: FileBackend, path: string, scope?: string) {
  const existing = remoteEditor.panels.find((p) => samePanel(p, path, scope))
  if (existing) {
    remoteEditor.activeId = existing.id
    return existing.id
  }
  const panel: RemotePanelInfo = { id: ++seq, title: basename(path) || path, rootPath: path, backend, scope }
  remoteEditor.panels.push(panel)
  remoteEditor.activeId = panel.id
  return panel.id
}

// 双击文件：在其所在目录的板块中打开（没有则自动创建）
export function openFileInEditor(backend: FileBackend, entry: { path: string }, scope?: string) {
  const dir = parentDir(entry.path, '/')
  openDirInEditor(backend, dir, scope)
  remoteEditor.pendingOpen = { path: entry.path }
}

export function closePanel(id: number) {
  const idx = remoteEditor.panels.findIndex((p) => p.id === id)
  if (idx < 0) return
  remoteEditor.panels.splice(idx, 1)
  if (remoteEditor.activeId === id) {
    remoteEditor.activeId = remoteEditor.panels[idx]?.id ?? remoteEditor.panels[idx - 1]?.id ?? null
  }
}

// 关闭某个来源作用域下的全部板块（如某个 SSH 标签被关闭 / 会话断开）。
export function closePanelsByScope(scope: string) {
  if (!scope) return
  const doomed = remoteEditor.panels.filter((p) => p.scope === scope)
  if (!doomed.length) return
  for (const p of doomed) {
    closePanel(p.id)
  }
}

export function closeAllPanels() {
  remoteEditor.panels = []
  remoteEditor.activeId = null
  remoteEditor.pendingOpen = null
}
