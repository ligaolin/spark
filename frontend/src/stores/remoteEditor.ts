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

// 右键目录：把指定目录加载到编辑器板块（已存在则激活）
export function openDirInEditor(backend: FileBackend, path: string) {
  const existing = remoteEditor.panels.find((p) => p.rootPath === path)
  if (existing) {
    remoteEditor.activeId = existing.id
    return existing.id
  }
  const panel: RemotePanelInfo = { id: ++seq, title: basename(path) || path, rootPath: path, backend }
  remoteEditor.panels.push(panel)
  remoteEditor.activeId = panel.id
  return panel.id
}

// 双击文件：在其所在目录的板块中打开（没有则自动创建）
export function openFileInEditor(backend: FileBackend, entry: { path: string }) {
  const dir = parentDir(entry.path, '/')
  openDirInEditor(backend, dir)
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

export function closeAllPanels() {
  remoteEditor.panels = []
  remoteEditor.activeId = null
  remoteEditor.pendingOpen = null
}
