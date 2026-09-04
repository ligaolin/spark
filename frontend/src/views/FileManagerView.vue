<template>
  <div class="fs-view">
    <!-- 头部：连接选择 -->
    <div class="fs-head">
      <el-select
        v-model="connId"
        placeholder="选择已保存的连接"
        size="small"
        clearable
        filterable
        style="width: 240px"
        :disabled="connected"
      >
        <el-option v-for="c in candidates" :key="c.id" :label="c.name" :value="c.id" />
      </el-select>
      <el-button size="small" type="primary" plain @click="dialogVisible = true" :disabled="connected">
        新建连接
      </el-button>
      <el-button size="small" type="primary" @click="connect" :disabled="!connId || connected">
        连接
      </el-button>
      <el-button size="small" type="danger" plain @click="disconnect" :disabled="!connected">
        断开
      </el-button>
      <el-tag v-if="connected" size="default" type="success" effect="dark">{{ sessionLabel }}</el-tag>
      <el-tag v-else size="default" type="info">未连接</el-tag>
    </div>

    <!-- 主体：远程文件面板（单栏）。编辑器为独立页面（远程编辑器），
         右键目录/文件在此打开后自动跳转 -->
    <div class="fs-body">
      <div class="fs-file-area">
        <FilePanel
          ref="remotePanel"
          :backend="remoteBackend"
          :title="mode === 'sftp' ? 'SFTP 远程' : 'FTP 远程'"
          show-mode
          multi-select
          dock-editor
          placeholder="远程目录，回车跳转"
          :connected="connected"
          :fav-key="connId || 0"
          @drop="onRemoteDrop"
          @action="onPanelAction"
        >
          <template #actions>
            <el-button size="small" type="primary" :disabled="!connected" @click="pickAndUpload">
              上传
            </el-button>
            <el-button size="small" :disabled="!connected" @click="pickDirAndUpload">
              上传目录
            </el-button>
            <el-button size="small" :disabled="!connected" @click="remotePanel?.download()">
              下载
            </el-button>
            <el-button size="small" :disabled="!connected" @click="remotePanel?.mkdir()">
              新建目录
            </el-button>
            <el-button size="small" :disabled="!connected" @click="remotePanel?.rename()">
              重命名
            </el-button>
            <el-button size="small" type="danger" plain :disabled="!connected" @click="remotePanel?.remove()">
              删除
            </el-button>
            <el-button v-if="mode === 'sftp'" size="small" :disabled="!connected" @click="remotePanel?.chmod()">
              权限
            </el-button>
            <span class="upload-hint">可从资源管理器拖文件/文件夹到面板上传</span>
          </template>
        </FilePanel>
      </div>
    </div>

    <!-- 底部传输队列 -->
    <TransferDock />

    <ConnectDialog
      v-model="dialogVisible"
      mode="connect"
      :conn-type="savedConnType"
      @connect="onQuickConnect"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Events } from '@wailsio/runtime'
import FilePanel from '../components/FilePanel.vue'
import TransferDock from '../components/TransferDock.vue'
import ConnectDialog from '../components/ConnectDialog.vue'
import { useConnectionsStore } from '../stores/connections'
import { useTransfersStore } from '../stores/transfers'
import { openDirInEditor, openFileInEditor, closePanelsByScope } from '../stores/remoteEditor'
import {
  LocalService,
  SFTPFileService,
  FTPFileService,
  EVENTS,
  makeConnectOptions,
  makeSavedConnection,
} from '../utils/wails'
import type { ConnectOptions, SavedConnection } from '../utils/wails'
import type { DropPayload, PanelAction } from '../types'
import { joinPath, makeSftpBackend, makeFtpBackend, type FileBackend } from '../utils/fileBackend'
import { resolveHostKeyIssue } from '../utils/hostkey'

const props = defineProps<{ mode: 'sftp' | 'ftp' }>()

const router = useRouter()
const connStore = useConnectionsStore()
const transfers = useTransfersStore()

const connId = ref<number>()
const dialogVisible = ref(false)
const remotePanel = ref<InstanceType<typeof FilePanel>>()

// 当前会话 id：后端适配器在调用时读取该值
const currentSessionId = ref('')
const sessionLabel = ref('')
const connected = computed(() => !!currentSessionId.value)

// sftp 模式复用 ssh 类型的已保存连接
const savedConnType = computed<'ssh' | 'ftp'>(() => (props.mode === 'sftp' ? 'ssh' : 'ftp'))

const candidates = computed(() => connStore.list.filter((c) => c.type === savedConnType.value))

// 远程后端：方法在调用时读取 currentSessionId.value，会话重建后无需重建对象
const remoteBackend: FileBackend = {
  kind: 'remote',
  label: props.mode === 'sftp' ? 'SFTP' : 'FTP',
  sep: '/',
  home: async () => {
    const p =
      props.mode === 'sftp'
        ? await SFTPFileService.Home(currentSessionId.value)
        : await FTPFileService.Home(currentSessionId.value)
    return p || '/'
  },
  list: async (p) => {
    const list =
      props.mode === 'sftp'
        ? await SFTPFileService.List(currentSessionId.value, p)
        : await FTPFileService.List(currentSessionId.value, p)
    return list ?? []
  },
  mkdir: (p) =>
    props.mode === 'sftp'
      ? SFTPFileService.Mkdir(currentSessionId.value, p)
      : FTPFileService.Mkdir(currentSessionId.value, p),
  rename: (a, b) =>
    props.mode === 'sftp'
      ? SFTPFileService.Rename(currentSessionId.value, a, b)
      : FTPFileService.Rename(currentSessionId.value, a, b),
  remove: (p, isDir) =>
    props.mode === 'sftp'
      ? SFTPFileService.Remove(currentSessionId.value, p)
      : FTPFileService.Remove(currentSessionId.value, p, isDir),
  chmod:
    props.mode === 'sftp'
      ? (p, m) => SFTPFileService.Chmod(currentSessionId.value, p, m)
      : undefined,
  upload: (l, r) =>
    props.mode === 'sftp'
      ? SFTPFileService.Upload(currentSessionId.value, l, r)
      : FTPFileService.Upload(currentSessionId.value, l, r),
  download: (r, l, isDir) =>
    props.mode === 'sftp'
      ? SFTPFileService.Download(currentSessionId.value, r, l)
      : FTPFileService.Download(currentSessionId.value, r, l, !!isDir),
  readFile: (p) =>
    props.mode === 'sftp'
      ? SFTPFileService.ReadFile(currentSessionId.value, p)
      : FTPFileService.ReadFile(currentSessionId.value, p),
  writeFile: (p, c) =>
    props.mode === 'sftp'
      ? SFTPFileService.WriteFile(currentSessionId.value, p, c)
      : FTPFileService.WriteFile(currentSessionId.value, p, c),
  search: async (d, p, m, opts) =>
    props.mode === 'sftp'
      ? ((await SFTPFileService.Search(currentSessionId.value, d, p, m, opts)) ?? [])
      : ((await FTPFileService.Search(currentSessionId.value, d, p, m, opts)) ?? []),
  replace: (d, p, r, m, opts) =>
    props.mode === 'sftp'
      ? SFTPFileService.Replace(currentSessionId.value, d, p, r, m, opts)
      : FTPFileService.Replace(currentSessionId.value, d, p, r, m, opts),
}

// ---------- 编辑器（独立页面「远程编辑器」，见 stores/remoteEditor） ----------

function basename(p: string): string {
  const parts = p.split(/[\\/]/)
  return parts[parts.length - 1] || p
}

// 本页自己的编辑器板块作用域：断开 / 会话结束 / 卸载时只关闭本页打开的板块，
// 不误伤 SSH 终端 SFTP 等其他来源打开的板块。
let fmSeq = 0
const pageScope = `filemanager-${props.mode}-${Date.now()}-${++fmSeq}`

// 打开编辑器板块时把当前会话固化为独立的快照后端：之后本页即使重新连接
// 别的会话，也不会让旧板块落到新会话上（避免编辑内容指向错误服务器）。
function snapshotBackend(): FileBackend {
  const sid = currentSessionId.value
  if (!sid) return remoteBackend
  return props.mode === 'sftp'
    ? makeSftpBackend(() => sid)
    : makeFtpBackend(() => sid)
}

// 右键「用编辑器打开目录」：把整个目录加载到编辑器板块并跳转过去
function openDirInEditorView(path: string) {
  openDirInEditor(snapshotBackend(), path, pageScope)
  void router.push('/remote-editor')
}

// 双击远程文件：在编辑器板块中打开并跳转过去
function openFileInEditorView(entry: { path: string; name: string }) {
  openFileInEditor(snapshotBackend(), entry, pageScope)
  void router.push('/remote-editor')
}

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

async function connect() {
  const conn = candidates.value.find((c) => c.id === connId.value)
  if (!conn) return
  await doConnect(optsFromConn(conn), conn.name)
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
          type: savedConnType.value,
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

async function doConnect(opts: ConnectOptions, label: string, hostKeyRetry = 0): Promise<boolean> {
  try {
    currentSessionId.value =
      props.mode === 'sftp'
        ? await SFTPFileService.Connect(opts)
        : await FTPFileService.Connect(opts)
    sessionLabel.value = label
    ElMessage.success(`已连接 ${label}`)
    await remotePanel.value?.goHome()
    return true
  } catch (e: any) {
    // SSH 主机密钥未信任 / 不匹配：询问用户后保存密钥并重试一次
    if (props.mode === 'sftp' && hostKeyRetry === 0) {
      const accepted = await resolveHostKeyIssue(e, opts)
      if (accepted) {
        return doConnect(opts, label, 1)
      }
    }
    ElMessage.error(`连接失败：${e?.message || e}`)
    return false
  }
}

async function disconnect() {
  const id = currentSessionId.value
  if (!id) return
  try {
    if (props.mode === 'sftp') {
      await SFTPFileService.Disconnect(id)
    } else {
      await FTPFileService.Disconnect(id)
    }
  } catch {
    /* ignore */
  }
  currentSessionId.value = ''
  sessionLabel.value = ''
  remotePanel.value?.clear()
  // 会话断开后本页打开的编辑器板块不再可用，一并关闭（强制，不弹确认）；
  // 其他来源（如 SSH 终端 SFTP）打开的板块不受影响。
  closePanelsByScope(pageScope)
  ElMessage.info('已断开连接')
}

// ---------- 上传 ----------

async function runTransfer(name: string, fn: () => Promise<void>) {
  try {
    await fn()
    transfers.complete(currentSessionId.value, 'upload', name)
    ElMessage.success(`上传完成：${name}`)
  } catch (e: any) {
    transfers.fail(currentSessionId.value, 'upload', name, e?.message || String(e))
    ElMessage.error(`上传失败：${e?.message || e}`)
  }
}

// 批量上传（按钮选择 / 拖拽文件）：全部结束后统一提示
async function uploadBatch(items: { path: string; name: string; isDir: boolean }[], remoteDirOverride?: string) {
  if (!items.length) return
  if (!currentSessionId.value) {
    ElMessage.warning('请先连接远程服务器')
    return
  }
  const remoteDir = remoteDirOverride || remotePanel.value?.currentPath
  if (!remoteDir) return
  let ok = 0
  const failed: string[] = []
  for (const it of items) {
    const target = joinPath(remoteDir, it.name, '/')
    const label = it.isDir ? `${it.name}/ (目录)` : it.name
    try {
      await remoteBackend.upload(it.path, target)
      transfers.complete(currentSessionId.value, 'upload', label)
      ok++
    } catch (e: any) {
      transfers.fail(currentSessionId.value, 'upload', label, e?.message || String(e))
      failed.push(it.name)
    }
  }
  await remotePanel.value?.refresh()
  if (failed.length === 0) {
    ElMessage.success(`全部上传完成（${ok} 项）`)
  } else {
    ElMessage.error(
      `上传完成：成功 ${ok} 项，失败 ${failed.length} 项（${failed.slice(0, 3).join('、')}${failed.length > 3 ? '…' : ''}）`,
    )
  }
}

async function pickAndUpload() {
  if (!currentSessionId.value) {
    ElMessage.warning('请先连接远程服务器')
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

// 选择本地目录整目录上传
async function pickDirAndUpload() {
  if (!currentSessionId.value) {
    ElMessage.warning('请先连接远程服务器')
    return
  }
  const remoteDir = remotePanel.value?.currentPath
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
  await runTransfer(`${name}/ (目录)`, () => remoteBackend.upload(dir, joinPath(remoteDir, name, '/')))
  await remotePanel.value?.refresh()
}

// ---------- 下载 ----------

// 选择保存目录；用户取消时回退到系统默认下载目录
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
  } catch {
    /* ignore */
  }
  return null
}

// 批量下载（文件或目录，目录递归）：全部结束后统一提示
async function downloadBatch(items: { path: string; name: string; isDir: boolean }[]) {
  if (!items.length) return
  if (!currentSessionId.value) {
    ElMessage.warning('请先连接远程服务器')
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
      await remoteBackend.download(it.path, target, it.isDir)
      transfers.complete(currentSessionId.value, 'download', label)
      ok++
    } catch (e: any) {
      transfers.fail(currentSessionId.value, 'download', label, e?.message || String(e))
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

// 拖到远程面板：操作系统文件 / 内部条目都走批量上传
async function onRemoteDrop(payload: DropPayload) {
  if (!currentSessionId.value) {
    ElMessage.warning('请先连接远程服务器')
    return
  }
  const remoteDir = payload.targetDir || remotePanel.value?.currentPath
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

// 面板右键 / 双击动作
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
      const rows = remotePanel.value?.selectedRows ?? []
      if (rows.length) {
        await downloadBatch(rows.map((r) => ({ path: r.path, name: r.name, isDir: r.isDir })))
      }
      break
    }
    case 'upload-entry':
    case 'upload-multi':
      // 单栏模式下不再有跨面板操作
      break
  }
}

transfers.bind()
connStore.load()

// 保活检测到连接断开时，后端发 session:closed 事件，这里清理会话状态
let unSessionClosed: (() => void) | null = null
unSessionClosed = Events.On(EVENTS.sessionClosed, (evt: any) => {
  const sc = evt.data
  if (sc && sc.sessionId === currentSessionId.value) {
    currentSessionId.value = ''
    sessionLabel.value = ''
    remotePanel.value?.clear()
    closePanelsByScope(pageScope)
    ElMessage.warning(sc.reason || '连接已断开，请重新连接')
  }
})

// 从连接管理页跳转时自动连接（keep-alive 下每次显示都会触发）
onActivated(async () => {
  const raw = sessionStorage.getItem('spark:auto-connect')
  if (!raw) return
  sessionStorage.removeItem('spark:auto-connect')
  try {
    const intent = JSON.parse(raw)
    if (intent.mode !== props.mode || !intent.conn) return
    const conn: SavedConnection = intent.conn
    connId.value = conn.id
    await doConnect(optsFromConn(conn), conn.name)
  } catch {
    /* ignore */
  }
})

onBeforeUnmount(() => {
  unSessionClosed?.()
  // 页面卸载：本页编辑器板块随会话一起关闭（其他来源的板块不受影响）
  closePanelsByScope(pageScope)
  const id = currentSessionId.value
  if (id) {
    if (props.mode === 'sftp') {
      SFTPFileService.Disconnect(id).catch(() => undefined)
    } else {
      FTPFileService.Disconnect(id).catch(() => undefined)
    }
    currentSessionId.value = ''
  }
})
</script>

<style scoped>
.fs-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
}

.fs-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.fs-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.fs-file-area {
  flex: 1;
  min-height: 0;
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