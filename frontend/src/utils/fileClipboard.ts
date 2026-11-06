import { shallowRef } from 'vue'

// 文件面板 / 编辑器目录树共用的剪贴板。只记录路径与后端身份，
// 真正的复制在目标面板里通过该后端执行，因此跨服务器粘贴会被拦截。
export interface FileClipboardEntry {
  path: string
  name: string
  isDir: boolean
  cut: boolean
  scope?: string
  // 文档管理（数据库节点）用 id 定位，路径仅用于展示
  docId?: number
}

export const fileClipboard = shallowRef<FileClipboardEntry | null>(null)
