// 新版本检查 / 更新控制器：状态机 + 与 UpdateService 绑定。
// 桌面端：Check → DownloadAndInstall → Restart（自动替换二进制并重启）。
// 安卓端：Check → DownloadApk（下载到本地）→ window.wails.installApk(path)（调系统安装器）。
// UpdateDialog.vue 只负责渲染本模块的状态。
import { reactive } from 'vue'
import { Events } from '@wailsio/runtime'
import { ElMessage } from 'element-plus'
import { UpdateService } from './wails'
import { isAndroidApp } from './platform'

export interface UpdateInfo {
  current: string
  latest: string
  hasUpdate: boolean
  name: string
  body: string
}

export const isAndroid = isAndroidApp()

export const updateState = reactive({
  visible: false,
  phase: 'idle' as 'idle' | 'checking' | 'ready' | 'downloading' | 'installed' | 'error',
  info: null as UpdateInfo | null,
  done: 0,
  total: 0,
  downloadError: '',
  downloadedPath: '',
})

let progressBound = false
function bindProgress() {
  if (progressBound) return
  progressBound = true
  // 下载进度（桌面内置 updater 与安卓手动下载都用这个事件名）
  Events.On('wails:updater:download-progress', (evt: any) => {
    const p = evt?.data
    if (!p) return
    updateState.done = Number(p.written) || 0
    updateState.total = Number(p.total) || 0
  })
  // 安卓安装结果 / 权限提示
  Events.On('android:apkInstall', (evt: any) => {
    const p = evt?.data
    if (!p) return
    if (p.needPermission) {
      ElMessage.warning('请在系统设置中允许「安装未知应用」，然后回到应用再点一次「安装」')
    } else if (p.error) {
      ElMessage.error(`安装失败：${p.error}`)
    }
  })
}

function reset() {
  updateState.phase = 'idle'
  updateState.info = null
  updateState.done = 0
  updateState.total = 0
  updateState.downloadError = ''
  updateState.downloadedPath = ''
}

// 检查更新。silent=true（应用启动时）检查过程不打扰用户，仅在有新版本时
// 弹出提示；手动检查（silent=false）立即显示检查中状态，失败也会提示。
export async function checkForUpdates(silent = false): Promise<void> {
  bindProgress()
  reset()
  if (!silent) {
    updateState.phase = 'checking'
    updateState.visible = true
  }
  try {
    const info = await UpdateService.CheckUpdate()
    if (!info) throw new Error('未获取到版本信息')
    if (!info.hasUpdate) {
      if (!silent) {
        ElMessage.success(`当前已是最新版本（${info.current}）`)
        updateState.visible = false
      }
      return
    }
    updateState.info = info
    updateState.phase = 'ready'
    updateState.visible = true
  } catch (e: any) {
    if (!silent) {
      ElMessage.error(`检查更新失败：${e?.message || e}`)
    }
  }
}

// 手动检查：检查并展示结果（含错误提示）
export function checkForUpdatesManual() {
  return checkForUpdates(false)
}

export async function downloadUpdate(): Promise<void> {
  if (!updateState.info || updateState.phase === 'downloading') return
  updateState.phase = 'downloading'
  updateState.downloadError = ''
  updateState.done = 0
  updateState.total = 0
  try {
    if (isAndroid) {
      updateState.downloadedPath = await UpdateService.DownloadApk()
    } else {
      await UpdateService.DownloadAndInstall()
    }
    updateState.phase = 'installed'
  } catch (e: any) {
    updateState.downloadError = e?.message || String(e)
    updateState.phase = 'ready'
  }
}

// 安卓：调起系统安装器
export function installUpdate(): void {
  const w = (window as any).wails
  if (!w || typeof w.installApk !== 'function') {
    ElMessage.error('当前环境不支持在线安装')
    return
  }
  const path = updateState.downloadedPath
  if (!path) {
    ElMessage.warning('请先下载更新')
    return
  }
  w.installApk(path)
}

// 桌面：替换二进制并重启
export async function restartUpdate(): Promise<void> {
  try {
    await UpdateService.Restart()
  } catch (e: any) {
    ElMessage.error(`重启更新失败：${e?.message || e}`)
  }
}

export async function openReleasePage() {
  try {
    await UpdateService.OpenReleasePage()
  } catch (e: any) {
    ElMessage.error(`打开发布页失败：${e?.message || e}`)
  }
}

export function closeUpdateDialog() {
  updateState.visible = false
}

export function formatBytes(n: number): string {
  if (!n || n <= 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const v = n / Math.pow(1024, i)
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}
