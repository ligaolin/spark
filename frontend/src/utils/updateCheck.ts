// 新版本检查 / 下载控制器：状态机 + 与 UpdateService 绑定的调用。
// UpdateDialog.vue 只负责渲染本模块的状态。
import { reactive } from 'vue'
import { Events } from '@wailsio/runtime'
import { ElMessage } from 'element-plus'
import { UpdateService } from './wails'

export interface UpdateInfo {
  current: string
  latest: string
  hasUpdate: boolean
  tag: string
  name: string
  releaseUrl: string
  assetUrl: string
  assetName: string
  assetSize: number
  publishedAt: string
  body: string
}

export const updateState = reactive({
  visible: false,
  phase: 'idle' as 'idle' | 'checking' | 'ready' | 'error' | 'downloading' | 'done',
  info: null as UpdateInfo | null,
  errorMsg: '',
  done: 0,
  total: 0,
  downloadedPath: '',
  downloadError: '',
})

let progressBound = false
function bindProgress() {
  if (progressBound) return
  progressBound = true
  Events.On('update:progress', (evt: any) => {
    const p = evt?.data
    if (!p) return
    updateState.done = Number(p.done) || 0
    updateState.total = Number(p.total) || 0
  })
}

function reset() {
  updateState.phase = 'idle'
  updateState.info = null
  updateState.errorMsg = ''
  updateState.done = 0
  updateState.total = 0
  updateState.downloadedPath = ''
  updateState.downloadError = ''
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
    if (!info) {
      throw new Error('未获取到版本信息')
    }
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
    const path = await UpdateService.DownloadUpdate()
    updateState.downloadedPath = path
    updateState.phase = 'done'
  } catch (e: any) {
    updateState.downloadError = e?.message || String(e)
    updateState.phase = 'ready'
  }
}

export async function revealDownload() {
  if (!updateState.downloadedPath) return
  try {
    await UpdateService.RevealInExplorer(updateState.downloadedPath)
  } catch (e: any) {
    ElMessage.error(`打开文件夹失败：${e?.message || e}`)
  }
}

export async function launchDownload() {
  if (!updateState.downloadedPath) return
  try {
    await UpdateService.LaunchApp(updateState.downloadedPath)
    updateState.visible = false
    ElMessage.success('已启动新版本，请关闭本程序后使用新版本')
  } catch (e: any) {
    ElMessage.error(`启动失败：${e?.message || e}`)
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