// 前端类型定义。
// 跨 Go 服务边界的类型（ConnectOptions / SavedConnection）直接复用绑定
// 生成的模型类（见 utils/wails.ts），保证与后端 DTO 结构一致。
// 这里只保留纯 UI 辅助类型与工具函数。

export interface FileEntry {
  name: string
  path: string
  size: number
  mode: string
  modTime: string
  isDir: boolean
  symlink: boolean
  linkTarget?: string
}

export interface TerminalOutput {
  sessionId: string
  data: string
}

export interface TerminalExit {
  sessionId: string
  code: number
  error?: string
}

export interface TransferProgress {
  sessionId: string
  op: string // upload | download
  name: string
  done: number
  total: number
}

// 搜索命中的结果：文件名匹配或文件内容匹配（内容匹配带行号与行文本）
export interface SearchResult {
  path: string
  name: string
  size: number
  modTime: string
  isDir: boolean
  lineNo?: number
  line?: string
}

// 搜索 / 替换选项：区分大小写、正则（正则仅对内容搜索生效）、排除 glob
export interface SearchOptions {
  caseSensitive: boolean
  useRegex: boolean
  exclude: string
}

// 替换结果：修改的文件数与替换的匹配总数
export interface ReplaceResult {
  files: number
  occurrences: number
}

// 拖拽传输负载：source 为拖拽来源，entries 为内部面板条目，paths 为操作系统文件路径
// targetDir：拖到某个目录行上时指定的目标目录（缺省为面板当前目录）
export interface DropPayload {
  source: 'local' | 'remote' | 'files'
  entries?: { path: string; name: string; isDir: boolean }[]
  paths?: string[]
  targetDir?: string
}

// 文件面板右键菜单动作（跨面板操作由父组件处理）
export interface PanelAction {
  action:
    | 'pick-upload'
    | 'pick-upload-dir'
    | 'upload-entry'
    | 'download-entry'
    | 'upload-multi'
    | 'download-multi'
    // 用编辑器打开（文档式编辑板块）：文件 / 目录
    | 'open-file'
    | 'open-in-editor'
  entry?: { path: string; name: string; isDir: boolean }
}

// 格式化文件大小
export function formatSize(bytes: number): string {
  if (!bytes && bytes !== 0) return '-'
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const v = bytes / Math.pow(1024, i)
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

// 格式化时间
export function formatTime(t: string): string {
  if (!t) return '-'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '-'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
