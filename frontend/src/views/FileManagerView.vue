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

    <!-- 主体：左右双栏 -->
    <div class="fs-body">
      <el-splitter>
        <el-splitter-panel size="50%" :min="220">
          <FilePanel
            ref="localPanel"
            :backend="localBackend"
            title="本地"
            :connected="connected"
            :fav-key="0"
            multi-select
            @drop="onLocalDrop"
            @action="onPanelAction"
          >
            <template #actions>
              <el-button
                size="small"
                type="primary"
                :disabled="!canUpload"
                @click="uploadSelected"
              >
                ⇧ 上传选中{{ uploadCount > 1 ? `（${uploadCount}）` : '' }}
              </el-button>
              <el-button size="small" @click="localPanel?.mkdir()">新建目录</el-button>
              <el-button size="small" @click="localPanel?.rename()">重命名</el-button>
              <el-button size="small" type="danger" plain @click="localPanel?.remove()">删除</el-button>
            </template>
          </FilePanel>
        </el-splitter-panel>
        <el-splitter-panel :min="220">
          <FilePanel
            ref="remotePanel"
            :backend="remoteBackend"
            :title="mode === 'sftp' ? 'SFTP 远程' : 'FTP 远程'"
            show-mode
            multi-select
            placeholder="远程目录，回车跳转"
            :connected="connected"
            :fav-key="connId || 0"
            @drop="onRemoteDrop"
            @action="onPanelAction"
          >
            <template #actions>
              <el-button
                size="small"
                type="primary"
                :disabled="!canDownload"
                @click="downloadSelected"
              >
                ⇩ 下载选中{{ downloadCount > 1 ? `（${downloadCount}）` : '' }}
              </el-button>
              <el-button size="small" :disabled="!connected" @click="pickAndUpload">
                上传
              </el-button>
              <el-button size="small" :disabled="!connected" @click="pickDirAndUpload">
                上传目录
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
            </template>
          </FilePanel>
        </el-splitter-panel>
      </el-splitter>
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
import { ElMessage } from 'element-plus'
import { Events } from '@wailsio/runtime'
import FilePanel from '../components/FilePanel.vue'
import TransferDock from '../components/TransferDock.vue'
import ConnectDialog from '../components/ConnectDialog.vue'
import { useConnectionsStore } from '../stores/connections'
import { useTransfersStore } from '../stores/transfers'
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
import { joinPath, makeLocalBackend, type FileBackend } from '../utils/fileBackend'
import { resolveHostKeyIssue } from '../utils/hostkey'

const props = defineProps<{ mode: 'sftp' | 'ftp' }>()

const connStore = useConnectionsStore()
const transfers = useTransfersStore()

const connId = ref<number>()
const dialogVisible = ref(false)
const localPanel = ref<InstanceType<typeof FilePanel>>()
const remotePanel = ref<InstanceType<typeof FilePanel>>()

// 当前会话 id：后端适配器在调用时读取该值
const currentSessionId = ref('')
const sessionLabel = ref('')
const connected = computed(() => !!currentSessionId.value)

// sftp 模式复用 ssh 类型的已保存连接
const savedConnType = computed<'ssh' | 'ftp'>(() => (props.mode === 'sftp' ? 'ssh' : 'ftp'))

const candidates = computed(() => connStore.list.filter((c) => c.type === savedConnType.value))

const localBackend = makeLocalBackend()

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
    await localPanel.value?.goHome()
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
  ElMessage.info('已断开连接')
}

// 支持文件和目录（目录递归传输）
const uploadCount = computed(() => {
  const rows = localPanel.value?.selectedRows
  if (rows && rows.length > 0) return rows.length
  return localPanel.value?.selected ? 1 : 0
})
const downloadCount = computed(() => {
  const rows = remotePanel.value?.selectedRows
  if (rows && rows.length > 0) return rows.length
  return remotePanel.value?.selected ? 1 : 0
})
const canUpload = computed(() => connected.value && uploadCount.value > 0)
const canDownload = computed(() => connected.value && downloadCount.value > 0)

function basename(p: string): string {
  const parts = p.split(/[\\/]/)
  return parts[parts.length - 1] || p
}

async function runTransfer(op: 'upload' | 'download', name: string, fn: () => Promise<void>) {
  try {
    await fn()
    transfers.complete(currentSessionId.value, op, name)
    ElMessage.success(`${op === 'upload' ? '上传' : '下载'}完成：${name}`)
  } catch (e: any) {
    transfers.fail(currentSessionId.value, op, name, e?.message || String(e))
    ElMessage.error(`${op === 'upload' ? '上传' : '下载'}失败：${e?.message || e}`)
  }
}

// 批量上传：逐项执行，全部结束后统一提示。remoteDirOverride 用于拖到指定目录时定向放置
async function uploadBatch(
  items: { path: string; name: string; isDir: boolean }[],
  remoteDirOverride?: string,
) {
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

// 上传本地面板选中的（可多选）文件或目录
async function uploadSelected() {
  const panel = localPanel.value
  if (!panel) return
  const rows = panel.selectedRows
  const items =
    rows.length > 0
      ? rows.map((r) => ({ path: r.path, name: r.name, isDir: r.isDir }))
      : panel.selected
        ? [{ path: panel.selected.path, name: panel.selected.name, isDir: panel.selected.isDir }]
        : []
  if (!items.length) {
    ElMessage.warning('请先选择要上传的文件或目录')
    return
  }
  await uploadBatch(items)
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
  await runTransfer('upload', `${name}/ (目录)`, () =>
    remoteBackend.upload(dir, joinPath(remoteDir, name, '/')),
  )
  await remotePanel.value?.refresh()
}

// 批量下载远程选中项（可多选）到本地当前目录
async function downloadSelected() {
  const panel = remotePanel.value
  if (!panel) return
  const rows = panel.selectedRows
  const items =
    rows.length > 0
      ? rows.map((r) => ({ path: r.path, name: r.name, isDir: r.isDir }))
      : panel.selected
        ? [{ path: panel.selected.path, name: panel.selected.name, isDir: panel.selected.isDir }]
        : []
  if (!items.length) {
    ElMessage.warning('请先选择要下载的文件或目录')
    return
  }
  const localDir = localPanel.value?.currentPath
  if (!localDir) return
  let ok = 0
  const failed: string[] = []
  for (const it of items) {
    const target = joinPath(localDir, it.name, '/')
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
  await localPanel.value?.refresh()
  if (failed.length === 0) {
    ElMessage.success(`全部下载完成（${ok} 项）`)
  } else {
    ElMessage.error(
      `下载完成：成功 ${ok} 项，失败 ${failed.length} 项（${failed.slice(0, 3).join('、')}${failed.length > 3 ? '…' : ''}）`,
    )
  }
}

// 拖到左侧本地面板：把远程条目下载到本地当前目录（或拖到指定目录行时下载到该目录）
async function onLocalDrop(payload: DropPayload) {
  if (payload.source !== 'remote' || !payload.entries?.length) return
  if (!currentSessionId.value) {
    ElMessage.warning('请先连接远程服务器')
    return
  }
  const localDir = payload.targetDir || localPanel.value?.currentPath
  if (!localDir) return
  for (const entry of payload.entries) {
    const target = joinPath(localDir, entry.name, '/')
    const label = entry.isDir ? `${entry.name}/ (目录)` : entry.name
    await runTransfer('download', label, () =>
      remoteBackend.download(entry.path, target, entry.isDir),
    )
  }
  await localPanel.value?.refresh()
}

// 拖到右侧远程面板：本地条目 / 操作系统文件上传到远程当前目录（或拖到指定目录行时上传到该目录）
async function onRemoteDrop(payload: DropPayload) {
  if (!currentSessionId.value) {
    ElMessage.warning('请先连接远程服务器')
    return
  }
  const remoteDir = payload.targetDir || remotePanel.value?.currentPath
  if (!remoteDir) return

  // 拖拽本地条目（可能是多选集合）与操作系统文件都走批量上传，结束后统一提示
  if (payload.source === 'local' && payload.entries?.length) {
    await uploadBatch(payload.entries, remoteDir)
  } else if (payload.source === 'files' && payload.paths?.length) {
    await uploadBatch(
      payload.paths.map((p) => ({ path: p, name: basename(p), isDir: false })),
      remoteDir,
    )
  }
}

// 右键菜单跨面板操作
async function onPanelAction(payload: PanelAction) {
  switch (payload.action) {
    case 'pick-upload':
      await pickAndUpload()
      break
    case 'pick-upload-dir':
      await pickDirAndUpload()
      break
    case 'upload-entry': {
      if (!payload.entry) break
      if (!currentSessionId.value) {
        ElMessage.warning('请先连接远程服务器')
        break
      }
      const remoteDir = remotePanel.value?.currentPath
      if (!remoteDir) break
      const target = joinPath(remoteDir, payload.entry.name, '/')
      const label = payload.entry.isDir ? `${payload.entry.name}/ (目录)` : payload.entry.name
      await runTransfer('upload', label, () =>
        remoteBackend.upload(payload.entry!.path, target),
      )
      await remotePanel.value?.refresh()
      break
    }
    case 'download-entry': {
      if (!payload.entry) break
      if (!currentSessionId.value) {
        ElMessage.warning('请先连接远程服务器')
        break
      }
      const localDir = localPanel.value?.currentPath
      if (!localDir) break
      const target = joinPath(localDir, payload.entry.name, '/')
      const label = payload.entry.isDir ? `${payload.entry.name}/ (目录)` : payload.entry.name
      await runTransfer('download', label, () =>
        remoteBackend.download(payload.entry!.path, target, payload.entry!.isDir),
      )
      await localPanel.value?.refresh()
      break
    }
    case 'upload-multi':
      await uploadSelected()
      break
    case 'download-multi':
      await downloadSelected()
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
}

.fs-body :deep(.el-splitter) {
  height: 100%;
}

.fs-body :deep(.el-splitter-panel) {
  min-width: 0;
}

.fs-body :deep(.el-splitter-panel:not(:last-child)) {
  padding-right: 8px;
}

</style>
