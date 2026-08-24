// 文件面板后端适配器：把 SFTP / FTP / 本地文件操作统一成同一个接口，
// 供 FilePanel 组件复用。
import { LocalService, SFTPFileService, FTPFileService } from './wails'
import type { FileEntry, SearchResult, SearchOptions, ReplaceResult } from '../types'

export interface FileBackend {
  kind: 'local' | 'remote'
  label: string
  sep: string // 路径分隔符
  home(): Promise<string>
  list(path: string): Promise<FileEntry[]>
  mkdir(path: string): Promise<void>
  rename(oldPath: string, newPath: string): Promise<void>
  remove(path: string, isDir: boolean): Promise<void>
  chmod?(path: string, mode: number): Promise<void>
  upload(localPath: string, remotePath: string): Promise<void>
  download(remotePath: string, localPath: string, isDir?: boolean): Promise<void>
  // 编辑器：读取 / 保存文本文件
  readFile(path: string): Promise<string>
  writeFile(path: string, content: string): Promise<void>
  // 搜索：按文件名或文件内容递归搜索（options：区分大小写 / 正则）
  search(dir: string, pattern: string, mode: 'name' | 'content', options: SearchOptions): Promise<SearchResult[]>
  // 替换：对内容搜索结果执行全局替换
  replace(dir: string, pattern: string, replacement: string, mode: 'name' | 'content', options: SearchOptions): Promise<ReplaceResult>
}

export function makeLocalBackend(): FileBackend {
  return {
    kind: 'local',
    label: '本地',
    sep: '/', // 展示用；本地路径由后端处理
    home: async () => (await LocalService.Home()) || '/',
    list: async (p) => (await LocalService.List(p)) ?? [],
    mkdir: (p) => LocalService.Mkdir(p),
    rename: (a, b) => LocalService.Rename(a, b),
    remove: (p) => LocalService.Remove(p),
    upload: async () => {},
    download: async () => {},
    readFile: (p) => LocalService.ReadFile(p),
    writeFile: (p, c) => LocalService.WriteFile(p, c),
    search: async (d, p, m, opts) => (await LocalService.Search(d, p, m, opts)) ?? [],
    replace: (d, p, r, m, opts) => LocalService.Replace(d, p, r, m, opts),
  }
}

// 远程后端适配器：把 SFTP / FTP 文件操作统一成同一个接口，供 FilePanel 复用。
// sessionId 用「getter」而不是固定值：连接成功后再调用面板方法（如 goHome）
// 时能读到最新的会话 id，避免"先拿到旧后端对象再连上新会话"的竞态。
export function makeSftpBackend(getSessionId: () => string): FileBackend {
  return {
    kind: 'remote',
    label: 'SFTP',
    sep: '/',
    home: async () => (await SFTPFileService.Home(getSessionId())) || '/',
    list: async (p) => (await SFTPFileService.List(getSessionId(), p)) ?? [],
    mkdir: (p) => SFTPFileService.Mkdir(getSessionId(), p),
    rename: (a, b) => SFTPFileService.Rename(getSessionId(), a, b),
    remove: (p) => SFTPFileService.Remove(getSessionId(), p),
    chmod: (p, m) => SFTPFileService.Chmod(getSessionId(), p, m),
    upload: (l, r) => SFTPFileService.Upload(getSessionId(), l, r),
    download: (r, l) => SFTPFileService.Download(getSessionId(), r, l),
    readFile: (p) => SFTPFileService.ReadFile(getSessionId(), p),
    writeFile: (p, c) => SFTPFileService.WriteFile(getSessionId(), p, c),
    search: async (d, p, m, opts) => (await SFTPFileService.Search(getSessionId(), d, p, m, opts)) ?? [],
    replace: (d, p, r, m, opts) => SFTPFileService.Replace(getSessionId(), d, p, r, m, opts),
  }
}

export function makeFtpBackend(getSessionId: () => string): FileBackend {
  return {
    kind: 'remote',
    label: 'FTP',
    sep: '/',
    home: async () => (await FTPFileService.Home(getSessionId())) || '/',
    list: async (p) => (await FTPFileService.List(getSessionId(), p)) ?? [],
    mkdir: (p) => FTPFileService.Mkdir(getSessionId(), p),
    rename: (a, b) => FTPFileService.Rename(getSessionId(), a, b),
    remove: (p, isDir) => FTPFileService.Remove(getSessionId(), p, isDir),
    upload: (l, r) => FTPFileService.Upload(getSessionId(), l, r),
    download: (r, l, isDir) => FTPFileService.Download(getSessionId(), r, l, !!isDir),
    readFile: (p) => FTPFileService.ReadFile(getSessionId(), p),
    writeFile: (p, c) => FTPFileService.WriteFile(getSessionId(), p, c),
    search: async (d, p, m, opts) => (await FTPFileService.Search(getSessionId(), d, p, m, opts)) ?? [],
    replace: (d, p, r, m, opts) => FTPFileService.Replace(getSessionId(), d, p, r, m, opts),
  }
}

// 计算父目录。兼容 POSIX（/）与 Windows（\）路径：
// 折叠连续分隔符、忽略末尾分隔符；到达根目录时返回根路径本身（调用方据此判断根）。
// 修复点：原先只按单分隔符截取，导致
//  - 末尾带 "/" 的路径（如 /a/b/）返回 /a/b/（带尾斜杠）发给 FTP 服务器报错
//  - Windows 本地路径（C:\Users\foo）按 "/" 切分得到 "/"，上级跳到了盘符根
//  - 相对路径（a）直接返回 "/"，跳到了错误的根
export function parentDir(path: string, sep: string): string {
  if (!path) return sep
  // 实际分隔符：优先按路径里出现的（兼容本地 Windows 路径与远程 POSIX 路径混用）
  const s = path.includes('/') ? '/' : path.includes('\\') ? '\\' : sep

  // UNC 路径（\\server\share\...）单独处理，避免折叠掉开头的双分隔符
  if (/^[\\/]{2}/.test(path)) {
    const parts = path.replace(/[\\/]+/g, s).split(s).filter(Boolean)
    // parts: [server, share, ...rest]；\\server\share 视为根
    if (parts.length <= 2) return path.replace(/[\\/]+$/, '')
    return s + s + parts.slice(0, parts.length - 1).join(s)
  }

  // 折叠连续分隔符、去掉末尾分隔符
  const cleaned = path.replace(/[\\/]+/g, s).replace(new RegExp(`\\${s}+$`), '')
  if (!cleaned) return s // 纯分隔符路径，即根

  // Windows 盘符：C: / C:\ / C:\foo
  const drive = cleaned.match(/^([A-Za-z]:)([\\/]?)(.*)$/)
  if (drive) {
    const prefix = drive[1]
    const rest = drive[3]
    if (!rest) return prefix + s // C: -> C:\（盘符根）
    const idx = rest.lastIndexOf(s)
    if (idx < 0) return prefix + s // C:\foo -> C:\
    return prefix + s + rest.slice(0, idx) // C:\foo\bar -> C:\foo
  }

  // POSIX 绝对路径：/a/b -> /a；/a -> /
  if (cleaned.startsWith(s)) {
    const rest = cleaned.slice(1)
    const idx = rest.lastIndexOf(s)
    if (idx < 0) return s
    return s + rest.slice(0, idx)
  }

  // 相对路径：a/b -> a；a -> 根（返回 sep）
  const idx = cleaned.lastIndexOf(s)
  if (idx < 0) return sep
  return cleaned.slice(0, idx)
}

// 拼接路径：自动匹配 base 的分隔符，避免混用 / 与 \，并折叠重复分隔符
export function joinPath(base: string, name: string, sep: string): string {
  const s = base.includes('/') ? '/' : base.includes('\\') ? '\\' : sep
  const trimmed = base.replace(/[\\/]+$/, '')
  if (!trimmed) return `${s}${name}` // 根路径直接拼接
  return `${trimmed}${s}${name}`
}
