<template>
  <div class="ftp-view">
    <div v-if="store.tabs.length === 0" class="empty-state">
      <el-icon :size="44" color="var(--border-strong)"><FolderOpened /></el-icon>
      <p>还没有 FTP 连接</p>
      <el-button type="primary" @click="openDialog">新建 FTP 连接</el-button>
    </div>

    <template v-else>
      <div class="tab-bar-wrap">
        <div class="tab-bar">
          <div v-for="tab in store.tabs" :key="tab.key" class="tab"
            :class="{ active: tab.key === store.activeKey }"
            @click="store.setActive(tab.key)"
            @contextmenu.prevent="onTabContext($event, tab)">
            <span class="tab-dot" :class="tab.status"></span>
            <span class="tab-title" :title="tab.title">{{ tab.title }}</span>
            <el-icon class="tab-close" @click.stop="handleRemoveTab(tab.key)">
              <Close />
            </el-icon>
          </div>
          <div class="tab-add" title="新建连接" @click="openDialog">
            <el-icon><Plus /></el-icon>
          </div>
        </div>
      </div>

      <div class="ftp-body">
        <div class="ftp-file-area">
          <KeepAlive :max="6">
            <FilePanel
              v-if="store.activeTab?.status === 'connected'"
              :key="store.activeKey"
              :ref="(el: any) => setPanelRef(store.activeKey, el)"
              :backend="activeBackend"
              :title="store.activeTab?.title || 'FTP 远程'"
              show-mode
              multi-select
              dock-editor
              placeholder="远程目录，回车跳转"
              :connected="true"
              :fav-key="store.activeTab?.connId || 0"
              @drop="onRemoteDrop"
              @action="onPanelAction"
            >
              <template #actions>
                <el-button size="small" type="primary" @click="pickAndUpload">上传</el-button>
                <el-button size="small" @click="pickDirAndUpload">上传目录</el-button>
                <el-button size="small" @click="activePanel?.download()">下载</el-button>
                <el-button size="small" @click="activePanel?.mkdir()">新建目录</el-button>
                <el-button size="small" @click="activePanel?.rename()">重命名</el-button>
                <el-button size="small" type="danger" plain @click="activePanel?.remove()">删除</el-button>
                <span class="upload-hint">可从资源管理器拖文件/文件夹到面板上传</span>
              </template>
            </FilePanel>
          </KeepAlive>

          <div v-if="store.activeTab && store.activeTab.status !== 'connected'" class="tab-placeholder">
            <template v-if="store.activeTab.status === 'connecting'">
              <el-icon class="is-loading" :size="32"><Loading /></el-icon>
              <p>正在连接 {{ store.activeTab.title }}…</p>
            </template>
            <template v-else-if="store.activeTab.status === 'closed'">
              <el-icon :size="32" color="var(--text-secondary)"><WarningFilled /></el-icon>
              <p>连接已断开：{{ store.activeTab.title }}</p>
              <p v-if="store.activeTab.error" class="sub">{{ store.activeTab.error }}</p>
              <el-button size="small" type="primary" @click="reconnectTab(store.activeTab)">重新连接</el-button>
            </template>
            <template v-else-if="store.activeTab.status === 'error'">
              <el-icon :size="32" color="var(--danger-color)"><CircleCloseFilled /></el-icon>
              <p>连接失败：{{ store.activeTab.title }}</p>
              <p v-if="store.activeTab.error" class="sub">{{ store.activeTab.error }}</p>
              <el-button size="small" type="primary" @click="retryTab(store.activeTab)">重试</el-button>
            </template>
          </div>
        </div>
      </div>
    </template>

    <TransferDock />
    <ConnectDialog v-model="dialogVisible" mode="connect" conn-type="ftp" @connect="onQuickConnect" />
    <ContextMenu v-model="tabCtxVisible" :x="tabCtxX" :y="tabCtxY" :items="tabCtxItems" @pick="onTabCtxPick" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onActivated, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Events } from '@wailsio/runtime'
import {
  Close,
  Plus,
  FolderOpened,
  Loading,
  WarningFilled,
  CircleCloseFilled,
  CloseBold,
  DArrowRight,
  CircleClose,
} from '@element-plus/icons-vue'
import FilePanel from '../components/FilePanel.vue'
import TransferDock from '../components/TransferDock.vue'
import ConnectDialog from '../components/ConnectDialog.vue'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import { useFtpStore, type FtpTab } from '../stores/ftp'
import { useConnectionsStore } from '../stores/connections'
import { useTransfersStore } from '../stores/transfers'
import { openDirInEditor, openFileInEditor, closePanelsByScope } from '../stores/remoteEditor'
import {
  LocalService,
  FTPFileService,
  EVENTS,
  makeConnectOptions,
  makeSavedConnection,
} from '../utils/wails'
import type { ConnectOptions, SavedConnection } from '../utils/wails'
import type { DropPayload, PanelAction } from '../types'
import { joinPath, makeFtpBackend, type FileBackend } from '../utils/fileBackend'

const router = useRouter()
const store = useFtpStore()
const connStore = useConnectionsStore()
const transfers = useTransfersStore()

const dialogVisible = ref(false)
const panelRefs = ref<Record<string, InstanceType<typeof FilePanel> | null>>({})

const activePanel = computed(() => {
  if (!store.activeKey) return null
  return panelRefs.value[store.activeKey] ?? null
})

const activeBackend = computed<FileBackend>(() => {
  const tab = store.activeTab
  if (!tab) {
    return makeFtpBackend(() => '')
  }
  return makeFtpBackend(() => tab.sessionId)
})

// 标签页右键菜单
const tabCtxVisible = ref(false)
const tabCtxX = ref(0)
const tabCtxY = ref(0)
const tabCtxItems = ref<(CtxItem | 'divider')[]>([])
const tabCtxTab = ref<FtpTab | null>(null)
const tabCtxIndex = ref(-1)

function setPanelRef(key: string, el: any) {
  panelRefs.value[key] = el
}

// ---------- 编辑器板块（远程编辑器） ----------

let fmSeq = 0
const pageScope = `ftp-${Date.now()}-${++fmSeq}`

function snapshotBackend(): FileBackend {
  const sid = store.activeTab?.sessionId || ''
  return makeFtpBackend(() => sid)
}

function openDirInEditorView(path: string) {
  openDirInEditor(snapshotBackend(), path, pageScope)
  void router.push('/remote-editor')
}

function openFileInEditorView(entry: { path: string; name: string }) {
  openFileInEditor(snapshotBackend(), entry, pageScope)
  void router.push('/remote-editor')
}

// ---------- 连接管理 ----------

function openDialog() {
  dialogVisible.value = true
}

async function doConnect(opts: ConnectOptions, label: string, existingKey?: string): Promise<boolean> {
  if (existingKey) store.markConnecting(existingKey)

  try {
    const sessionId = await FTPFileService.Connect(opts)
    if (existingKey && store.tabs.find((t) => t.key === existingKey)) {
      store.markConnected(existingKey, sessionId)
    } else {
      const tab = store.addTab(opts, undefined, label)
      store.markConnected(tab.key, sessionId)
    }
    ElMessage.success(`已连接 ${label}`)
    await nextTick()
    const key = existingKey || store.activeKey
    if (key) {
      await panelRefs.value[key]?.goHome()
    }
    return true
  } catch (e: any) {
    if (existingKey && store.tabs.find((t) => t.key === existingKey)) {
      store.markError(existingKey, e?.message || String(e))
    } else {
      const tab = store.addTab(opts, undefined, label)
      store.markError(tab.key, e?.message || String(e))
    }
    ElMessage.error(`连接失败：${e?.message || e}`)
    return false
  }
}

async function onQuickConnect(opts: ConnectOptions, save: boolean) {
  dialogVisible.value = false
  const label = `${opts.username}@${opts.host}:${opts.port}`
  const ok = await doConnect(opts, label)
  if (ok && save) {
    try {
      await connStore.create(
        makeSavedConnection({
          name: label,
          type: 'ftp',
          host: opts.host,
          port: opts.port,
          username: opts.username,
          password: opts.password,
          useKey: opts.useKey,
          privateKey: opts.privateKey,
          passphrase: opts.passphrase,
          forwardAgent: opts.forwardAgent,
          defaultDir: opts.defaultDir || '',
          tls: !!opts.tls,
        }),
      )
      ElMessage.success('已保存到连接列表')
    } catch (e: any) {
      ElMessage.error(`保存连接失败：${e?.message || e}`)
    }
  }
}

async function handleRemoveTab(key: string) {
  const tab = store.tabs.find((t) => t.key === key)
  if (!tab) return
  if (tab.sessionId) {
    try {
      await FTPFileService.Disconnect(tab.sessionId)
    } catch { /* ignore */ }
  }
  store.removeTab(key)
  delete panelRefs.value[key]
  if (store.tabs.length === 0) {
    closePanelsByScope(pageScope)
  }
}

async function reconnectTab(tab: FtpTab) {
  store.markConnecting(tab.key)
  await doConnect(tab.opts, tab.title, tab.key)
}

async function retryTab(tab: FtpTab) {
  store.markConnecting(tab.key)
  await doConnect(tab.opts, tab.title, tab.key)
}

// ---------- 标签页右键菜单 ----------

function onTabContext(event: MouseEvent, tab: FtpTab) {
  event.preventDefault()
  tabCtxTab.value = tab
  tabCtxIndex.value = store.tabs.findIndex((t) => t.key === tab.key)
  tabCtxItems.value = buildTabCtx()
  tabCtxX.value = event.clientX
  tabCtxY.value = event.clientY
  tabCtxVisible.value = false
  requestAnimationFrame(() => {
    tabCtxVisible.value = true
  })
}

function buildTabCtx(): (CtxItem | 'divider')[] {
  const total = store.tabs.length
  const idx = tabCtxIndex.value
  return [
    { key: 'reconnect', label: '重新连接', icon: Close, disabled: false },
    { key: 'close-current', label: '关闭当前', icon: Close, disabled: total === 0 },
    { key: 'close-others', label: '关闭其他', icon: CloseBold, disabled: total <= 1 },
    { key: 'close-right', label: '关闭右边', icon: DArrowRight, disabled: idx < 0 || idx >= total - 1 },
    { key: 'close-all', label: '关闭全部', icon: CircleClose, disabled: total === 0 },
  ]
}

async function onTabCtxPick(item: CtxItem) {
  const tab = tabCtxTab.value
  if (!tab) return
  const idx = store.tabs.findIndex((t) => t.key === tab.key)
  switch (item.key) {
    case 'reconnect':
      await reconnectTab(tab)
      break
    case 'close-current':
      await handleRemoveTab(tab.key)
      break
    case 'close-others':
      for (const t of store.tabs.filter((t) => t.key !== tab.key)) {
        await handleRemoveTab(t.key)
      }
      break
    case 'close-right':
      if (idx >= 0) {
        for (const t of store.tabs.slice(idx + 1)) {
          await handleRemoveTab(t.key)
        }
      }
      break
    case 'close-all':
      for (const t of [...store.tabs]) {
        await handleRemoveTab(t.key)
      }
      break
  }
}

// ---------- 上传 ----------

function basename(p: string): string {
  const parts = p.split(/[\\/]/)
  return parts[parts.length - 1] || p
}

async function uploadBatch(items: { path: string; name: string; isDir: boolean }[], remoteDirOverride?: string) {
  if (!items.length) return
  const tab = store.activeTab
  if (!tab || !tab.sessionId) {
    ElMessage.warning('请先连接 FTP 服务器')
    return
  }
  const remoteDir = remoteDirOverride || activePanel.value?.currentPath
  if (!remoteDir) return
  let ok = 0
  const failed: string[] = []
  for (const it of items) {
    const target = joinPath(remoteDir, it.name, '/')
    const label = it.isDir ? `${it.name}/ (目录)` : it.name
    try {
      await activeBackend.value.upload(it.path, target)
      transfers.complete(tab.sessionId, 'upload', label)
      ok++
    } catch (e: any) {
      transfers.fail(tab.sessionId, 'upload', label, e?.message || String(e))
      failed.push(it.name)
    }
  }
  await activePanel.value?.refresh()
  if (failed.length === 0) {
    ElMessage.success(`全部上传完成（${ok} 项）`)
  } else {
    ElMessage.error(
      `上传完成：成功 ${ok} 项，失败 ${failed.length} 项（${failed.slice(0, 3).join('、')}${failed.length > 3 ? '…' : ''}）`,
    )
  }
}

async function pickAndUpload() {
  const tab = store.activeTab
  if (!tab || !tab.sessionId) {
    ElMessage.warning('请先连接 FTP 服务器')
    return
  }
  let files: string[]
  try {
    files = (await LocalService.PickFiles()) ?? []
  } catch (e: any) {
    ElMessage.error(`选择文件失败：${e?.message || e}`)
    return
  }
  if (!files || files.length === 0) return
  await uploadBatch(files.map((f) => ({ path: f, name: basename(f), isDir: false })))
}

async function pickDirAndUpload() {
  const tab = store.activeTab
  if (!tab || !tab.sessionId) {
    ElMessage.warning('请先连接 FTP 服务器')
    return
  }
  const remoteDir = activePanel.value?.currentPath
  if (!remoteDir) return
  let dir: string
  try {
    dir = (await LocalService.PickDirectory()) || ''
  } catch (e: any) {
    ElMessage.error(`选择目录失败：${e?.message || e}`)
    return
  }
  if (!dir) return
  const name = basename(dir)
  const target = joinPath(remoteDir, name, '/')
  try {
    await activeBackend.value.upload(dir, target)
    transfers.complete(tab.sessionId, 'upload', `${name}/ (目录)`)
    ElMessage.success(`上传完成：${name}`)
  } catch (e: any) {
    transfers.fail(tab.sessionId, 'upload', `${name}/ (目录)`, e?.message || String(e))
    ElMessage.error(`上传失败：${e?.message || e}`)
  }
  await activePanel.value?.refresh()
}

// ---------- 拖放 ----------

async function onRemoteDrop(payload: DropPayload) {
  const tab = store.activeTab
  if (!tab || !tab.sessionId) {
    ElMessage.warning('请先连接 FTP 服务器')
    return
  }
  const remoteDir = payload.targetDir || activePanel.value?.currentPath
  if (!remoteDir) return

  if (payload.source === 'local' && payload.entries?.length) {
    await uploadBatch(payload.entries, remoteDir)
  } else if (payload.source === 'files' && payload.paths?.length) {
    await uploadBatch(
      payload.paths.map((p) => ({ path: p, name: basename(p), isDir: false })),
      remoteDir,
    )
  }
}

// ---------- 面板动作 ----------

async function onPanelAction(payload: PanelAction) {
  switch (payload.action) {
    case 'pick-upload':
      await pickAndUpload()
      break
    case 'pick-upload-dir':
      await pickDirAndUpload()
      break
    case 'open-in-editor':
      if (payload.entry?.isDir) openDirInEditorView(payload.entry.path)
      break
    case 'open-file':
      if (payload.entry) openFileInEditorView(payload.entry)
      break
    case 'download-entry':
      if (payload.entry) await downloadBatch([payload.entry])
      break
    case 'download-multi': {
      const rows = activePanel.value?.selectedRows ?? []
      if (rows.length) {
        await downloadBatch(rows.map((r) => ({ path: r.path, name: r.name, isDir: r.isDir })))
      }
      break
    }
    case 'upload-entry':
    case 'upload-multi':
      break
  }
}

// ---------- 下载 ----------

async function pickDownloadDir(): Promise<string | null> {
  try {
    const dir = await LocalService.PickDirectory()
    if (dir) return dir
  } catch (e: any) {
    ElMessage.error(`选择目录失败：${e?.message || e}`)
  }
  try {
    const def = await LocalService.DefaultDownloadDir()
    if (def) {
      ElMessage.info(`未选择保存目录，使用系统下载目录：${def}`)
      return def
    }
  } catch { /* ignore */ }
  return null
}

async function downloadBatch(items: { path: string; name: string; isDir: boolean }[]) {
  if (!items.length) return
  const tab = store.activeTab
  if (!tab || !tab.sessionId) {
    ElMessage.warning('请先连接 FTP 服务器')
    return
  }
  const dir = await pickDownloadDir()
  if (!dir) return
  let ok = 0
  const failed: string[] = []
  for (const it of items) {
    const target = joinPath(dir, it.name, '/')
    const label = it.isDir ? `${it.name}/ (目录)` : it.name
    try {
      await activeBackend.value.download(it.path, target, it.isDir)
      transfers.complete(tab.sessionId, 'download', label)
      ok++
    } catch (e: any) {
      transfers.fail(tab.sessionId, 'download', label, e?.message || String(e))
      failed.push(it.name)
    }
  }
  if (failed.length === 0) {
    ElMessage.success(`全部下载完成（${ok} 项），保存位置：${dir}`)
  } else {
    ElMessage.error(
      `下载完成：成功 ${ok} 项，失败 ${failed.length} 项（${failed.slice(0, 3).join('、')}${failed.length > 3 ? '…' : ''}）`,
    )
  }
}

// ---------- 生命周期 ----------

transfers.bind()
connStore.load()

// 保活检测到连接断开时处理
let unSessionClosed: (() => void) | null = null
unSessionClosed = Events.On(EVENTS.sessionClosed, (evt: any) => {
  const sc = evt.data
  if (!sc || !sc.sessionId) return
  const tab = store.tabs.find((t) => t.sessionId === sc.sessionId)
  if (tab) {
    store.markClosed(tab.key, 0, sc.reason || '连接已断开')
    ElMessage.warning(sc.reason || `${tab.title} 连接已断开，请重新连接`)
  }
})

// 从连接管理页跳转时自动连接
onActivated(async () => {
  const raw = sessionStorage.getItem('spark:auto-connect')
  if (!raw) return
  sessionStorage.removeItem('spark:auto-connect')
  try {
    const intent = JSON.parse(raw)
    if (intent.mode !== 'ftp' || !intent.conn) return
    const conn: SavedConnection = intent.conn
    await doConnect(optsFromConn(conn), conn.name)
  } catch { /* ignore */ }
})

function optsFromConn(conn: SavedConnection): ConnectOptions {
  return makeConnectOptions({
    host: conn.host,
    port: conn.port,
    username: conn.username,
    password: conn.password,
    useKey: conn.useKey,
    privateKey: conn.privateKey,
    passphrase: conn.passphrase,
    forwardAgent: conn.forwardAgent,
    defaultDir: conn.defaultDir,
    tls: conn.tls,
    insecure: false,
  })
}

onBeforeUnmount(() => {
  unSessionClosed?.()
  closePanelsByScope(pageScope)
  for (const tab of store.tabs) {
    if (tab.sessionId) {
      FTPFileService.Disconnect(tab.sessionId).catch(() => undefined)
    }
  }
  store.tabs = []
  store.activeKey = ''
})
</script>

<style scoped>
.ftp-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
}

.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--text-secondary);
}

.empty-state p {
  margin: 0;
  font-size: 14px;
}

.tab-bar-wrap {
  flex-shrink: 0;
}

.tab-bar {
  display: flex;
  align-items: center;
  gap: 2px;
  border-bottom: 1px solid var(--border-color);
  padding-bottom: 2px;
}

.tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  font-size: 12.5px;
  color: var(--text-secondary);
  background: var(--panel-soft);
  border: 1px solid var(--border-color);
  border-bottom: none;
  border-radius: 6px 6px 0 0;
  cursor: pointer;
  max-width: 240px;
  flex-shrink: 0;
  user-select: none;
  transition: background 0.15s;
}

.tab:hover {
  background: var(--hover-bg);
}

.tab.active {
  background: var(--active-bg);
  color: var(--active-text);
  border-color: var(--border-color);
}

.tab-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--text-secondary);
}

.tab-dot.connected {
  background: #67c23a;
}

.tab-dot.connecting {
  background: #e6a23c;
  animation: pulse 1s infinite;
}

.tab-dot.closed,
.tab-dot.error {
  background: #f56c6c;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

.tab-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.tab-close {
  font-size: 12px;
  color: var(--text-secondary);
  border-radius: 3px;
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 0.15s;
}

.tab:hover .tab-close,
.tab.active .tab-close {
  opacity: 1;
}

.tab-close:hover {
  color: #f56c6c;
  background: rgba(245, 108, 108, 0.15);
}

.tab-add {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 28px;
  border-radius: 5px;
  cursor: pointer;
  color: var(--text-secondary);
  flex-shrink: 0;
  margin-left: 2px;
}

.tab-add:hover {
  background: var(--hover-bg);
  color: var(--active-text);
}

.ftp-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ftp-file-area {
  flex: 1;
  min-height: 0;
}

.tab-placeholder {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--text-secondary);
  background: var(--panel-bg);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  min-height: 300px;
}

.tab-placeholder p {
  margin: 0;
  font-size: 13px;
}

.tab-placeholder .sub {
  font-size: 12px;
  opacity: 0.75;
  max-width: 400px;
  text-align: center;
}

.upload-hint {
  margin-left: auto;
  font-size: 11.5px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>