import { ElMessage } from 'element-plus'
import router from './router'

// 传输提示只在发起它的页面可见，避免 SFTP 的上传结果冒到 FTP 页面上。
const scopeRoutes: Record<string, string[]> = {
  'terminal-sftp': ['/terminal'],
  'terminal-sftp-editor': ['/remote-editor'],
  'ftp': ['/ftp'],
  'file-sftp': ['/sftp'],
  'file-ftp': ['/ftp'],
  editor: ['/remote-editor'],
  local: [],
}

type NotifyType = 'success' | 'warning' | 'error' | 'info'

export function notifyScope(scope: string, type: NotifyType, message: string): void {
  const routes = scopeRoutes[scope]
  if (routes && routes.length > 0 && !routes.includes(router.currentRoute.value.path)) return
  ElMessage[type]({ message, grouping: true })
}
