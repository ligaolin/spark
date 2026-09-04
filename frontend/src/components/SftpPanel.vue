<template>
    <div class="sftp-panel">
        <div v-if="!opts" class="sftp-empty">
            <el-icon :size="30" color="var(--border-strong)">
                <FolderOpened />
            </el-icon>
            <p>打开 SSH 会话后，此面板自动连接 SFTP</p>
        </div>

        <template v-else>
            <div class="sftp-file-area">
                <FilePanel ref="remotePanel" :backend="remoteBackend" title="SFTP 远程" show-mode multi-select dock-editor
                    placeholder="远程目录，回车跳转" :connected="connected" :fav-key="favKey" @drop="onRemoteDrop"
                    @action="onPanelAction">
                    <template #head>
                        <span v-if="error" class="sftp-err" :title="error">{{ error }}</span>
                        <el-button v-if="error" size="small" type="primary" @click="reconnect">重试</el-button>
                    </template>

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
                        <el-button size="small" type="danger" plain :disabled="!connected"
                            @click="remotePanel?.remove()">
                            删除
                        </el-button>
                        <el-button size="small" :disabled="!connected" @click="remotePanel?.chmod()">
                            权限
                        </el-button>
                        <!-- <span class="sftp-hint">双击文件在「远程编辑器」打开；可拖文件/文件夹到面板上传；下载未选目录时用系统下载目录</span> -->
                    </template>
                </FilePanel>
            </div>

            <TransferDock />
        </template>
    </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Events } from '@wailsio/runtime'
import { FolderOpened } from '@element-plus/icons-vue'
import FilePanel from './FilePanel.vue'
import TransferDock from './TransferDock.vue'
import { useTransfersStore } from '../stores/transfers'
import { openDirInEditor, openFileInEditor, closePanelsByScope } from '../stores/remoteEditor'
import { SFTPFileService, EVENTS, LocalService } from '../utils/wails'
import type { ConnectOptions } from '../utils/wails'
import type { DropPayload, PanelAction } from '../types'
import { joinPath, makeSftpBackend, type FileBackend } from '../utils/fileBackend'
import { resolveHostKeyIssue } from '../utils/hostkey'
import { useTerminalStore } from '../stores/terminal'

const props = defineProps<{
    // 当前活动 SSH 标签的连接参数；为空（无标签）时断开并显示引导
    opts: ConnectOptions | null
    // 当前活动 SSH 标签 key：每个标签各自持有独立的 SFTP 会话
    tabKey: string
    // 目录收藏键（来源已保存连接 id；快速连接为 0 时不展示收藏）
    favKey?: number
}>()

const router = useRouter()
const transfers = useTransfersStore()
const terminalStore = useTerminalStore()
const remotePanel = ref<InstanceType<typeof FilePanel>>()

// 每个 SSH 标签一个独立 SFTP 会话 + 后端对象。
// 关键修复：切换活动标签只切换「当前显示」，旧标签的会话保持存活，
// 因此远程编辑器里从该标签打开的文件 / 目录板块不会在切换标签时被重置；
// 只有标签被真正关闭或会话断开时，才会随标签一起清理。
interface TabSftp {
    opts: ConnectOptions | null
    id: string
    error: string
    // 递增序号：作废在途的连接结果（标签被关闭 / 有更新的连接请求时）
    seq: number
    pending: Promise<void> | null
    backend: FileBackend | null
}

const tabs = reactive<Record<string, TabSftp>>({})

function ensureTab(key: string, opts?: ConnectOptions | null) {
    if (!key) return
    if (!tabs[key]) {
        tabs[key] = { opts: opts ?? null, id: '', error: '', seq: 0, pending: null, backend: null }
    } else if (opts) {
        tabs[key].opts = opts
    }
}

const sessionId = computed(() => {
    const key = props.tabKey
    return key && tabs[key] ? tabs[key].id : ''
})
const connected = computed(() => !!sessionId.value)
const error = computed(() => {
    const key = props.tabKey
    return key && tabs[key] ? tabs[key].error : ''
})

// ---------- 会话丢失自动重连（仅针对其所属标签重连） ----------

function isSessionGone(e: any): boolean {
    const m = String(e?.message || e)
    return m.includes('会话') && (m.includes('不存在') || m.includes('已关闭'))
}

// 给某个标签拨一个全新 SFTP 会话（不做标签间切换的清理）。
async function doConnect(key: string, opts: ConnectOptions, seq: number, hostKeyRetry: number): Promise<void> {
    const t = tabs[key]
    try {
        const id = await SFTPFileService.Connect(opts)
        if (tabs[key] !== t || seq !== t.seq) {
            // 期间标签被关闭 / 有更新的连接请求：清理掉这个多余会话
            SFTPFileService.Disconnect(id).catch(() => undefined)
            return
        }
        t.error = ''
        t.id = id
    } catch (e: any) {
        if (tabs[key] !== t || seq !== t.seq) return
        if (hostKeyRetry === 0) {
            const accepted = await resolveHostKeyIssue(e, opts)
            if (accepted) {
                await doConnect(key, opts, ++t.seq, 1)
                return
            }
        }
        t.error = e?.message || String(e)
    }
}

// 确保 key 标签已有会话：已连接直接返回 true；未连接则发起拨号
// （并发请求复用同一次拨号）。
async function connectTab(key: string, hostKeyRetry = 0): Promise<boolean> {
    ensureTab(key)
    const t = tabs[key]
    if (!t) return false
    if (t.id) return true
    if (t.pending) {
        try {
            await t.pending
        } catch {
            /* 拨号内部已处理错误 */
        }
        return !!tabs[key]?.id
    }
    const opts = t.opts
    if (!opts) return false
    const seq = ++t.seq
    const p = doConnect(key, opts, seq, hostKeyRetry)
    t.pending = p
    try {
        await p
    } finally {
        if (tabs[key] === t && t.pending === p) t.pending = null
    }
    return !!tabs[key]?.id
}

// 每个标签一个稳定后端对象（sessionId 用 getter 实时读取该标签当前会话，
// 保存到编辑器板块里的后端不会因切换到其他标签而指向别的会话）。
function buildTabBackend(key: string, opts: ConnectOptions): FileBackend {
    const raw = makeSftpBackend(() => (tabs[key] ? tabs[key].id : ''))
    const wrap = (fn: (...args: any[]) => Promise<any>) =>
        async (...args: any[]) => {
            try {
                return await fn(...args)
            } catch (e: any) {
                if (isSessionGone(e) && (await connectTab(key))) {
                    return await fn(...args)
                }
                throw e
            }
        }
    return {
        ...raw,
        home: wrap(raw.home),
        list: wrap(raw.list),
        mkdir: wrap(raw.mkdir),
        rename: wrap(raw.rename),
        remove: wrap(raw.remove),
        chmod: wrap(raw.chmod!),
        upload: wrap(raw.upload),
        download: wrap(raw.download),
        readFile: wrap(raw.readFile),
        writeFile: wrap(raw.writeFile),
        search: wrap(raw.search),
        replace: wrap(raw.replace),
    }
}

function backendForTab(key: string): FileBackend | null {
    if (!key || !props.opts && !tabs[key]?.opts) return null
    ensureTab(key, props.opts)
    const t = tabs[key]
    if (!t || !t.opts) return null
    if (!t.backend) t.backend = buildTabBackend(key, t.opts)
    return t.backend
}

// FilePanel 当前展示用的后端（随活动标签切换）
const remoteBackend = computed<FileBackend>(() => backendForTab(props.tabKey) ?? nullBackend)

// 无活动标签时的空后端（模板此时显示空态，不会真正使用）
const nullBackend: FileBackend = {
    kind: 'remote',
    label: 'SFTP',
    sep: '/',
    home: async () => '/',
    list: async () => [],
    mkdir: async () => undefined,
    rename: async () => undefined,
    remove: async () => undefined,
    upload: async () => undefined,
    download: async () => undefined,
    readFile: async () => '',
    writeFile: async () => undefined,
    search: async () => [],
    replace: async () => ({ files: 0, occurrences: 0 }),
}

// ---------- 标签生命周期 ----------

// 活动标签切换：只把面板视图切到该标签的会话，不关闭旧标签会话与编辑器板块
async function activateTab(key: string) {
    if (!key) {
        remotePanel.value?.clear()
        return
    }
    ensureTab(key, props.opts)
    const ok = await connectTab(key)
    if (props.tabKey !== key) return // 拨号期间又切换了标签，交给最新的激活流程
    remotePanel.value?.clear()
    if (ok) {
        await panelGoHome()
    }
}

// 让 FilePanel 回到当前标签的主目录；面板尚未挂载时等一帧再试
// （如 keep-alive 恢复瞬间、首次拨号比面板渲染更快完成）
async function panelGoHome() {
    let panel = remotePanel.value
    if (!panel) {
        await nextTick()
        panel = remotePanel.value
    }
    await panel?.goHome()
}

// 标签被关闭 / 移除：断开其 SFTP 会话并关闭属于该标签的编辑器板块
function teardownTab(key: string) {
    const t = tabs[key]
    if (!t) return
    t.seq++ // 作废在途拨号
    if (t.id) {
        SFTPFileService.Disconnect(t.id).catch(() => undefined)
    }
    delete tabs[key]
    // 该标签自己的编辑器板块随之关闭（不触碰其他标签 / FTP 页的板块）
    closePanelsByScope(key)
    if (props.tabKey === key) {
        remotePanel.value?.clear()
    }
}

watch(
    () => props.tabKey,
    (key) => {
        void activateTab(key || '')
    },
    { immediate: true },
)

// 标签列表变化 → 清理已被移除标签的会话（关闭其他标签时也要释放）
watch(
    () => terminalStore.tabs.map((t) => t.key),
    (keys) => {
        const current = new Set(keys)
        for (const key of Object.keys(tabs)) {
            if (!current.has(key)) teardownTab(key)
        }
    },
)

// 保活检测到连接断开（任何标签的会话）→ 只清理该标签
let unSessionClosed: (() => void) | null = null
unSessionClosed = Events.On(EVENTS.sessionClosed, (evt: any) => {
    const sc = evt.data
    if (!sc || !sc.sessionId) return
    for (const key of Object.keys(tabs)) {
        const t = tabs[key]
        if (!t || t.id !== sc.sessionId) continue
        t.seq++
        t.id = ''
        t.pending = null
        closePanelsByScope(key)
        if (key === props.tabKey) {
            remotePanel.value?.clear()
            t.error = sc.reason || '连接已断开，请重新连接'
        }
        return
    }
})

function reconnect() {
    const key = props.tabKey
    if (!key) return
    const t = tabs[key]
    if (t) t.error = ''
    void connectTab(key).then((ok) => {
        if (ok) void panelGoHome()
    })
}

onBeforeUnmount(() => {
    unSessionClosed?.()
    // 逐个关闭仍由本组件持有的标签会话（各自的编辑器板块随之按 scope 关闭）
    for (const key of Object.keys(tabs)) teardownTab(key)
})

transfers.bind()

// ---------- 上传 ----------

function basename(p: string): string {
    const parts = p.split(/[\\/]/)
    return parts[parts.length - 1] || p
}

function runTransfer(name: string, fn: () => Promise<void>) {
    return fn()
        .then(() => {
            transfers.complete(sessionId.value, 'upload', name)
            ElMessage.success(`上传完成：${name}`)
        })
        .catch((e: any) => {
            transfers.fail(sessionId.value, 'upload', name, e?.message || String(e))
            ElMessage.error(`上传失败：${e?.message || e}`)
        })
}

async function uploadBatch(items: { path: string; name: string; isDir: boolean }[], remoteDirOverride?: string) {
    if (!items.length) return
    if (!sessionId.value) {
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
            await remoteBackend.value.upload(it.path, target)
            transfers.complete(sessionId.value, 'upload', label)
            ok++
        } catch (e: any) {
            transfers.fail(sessionId.value, 'upload', label, e?.message || String(e))
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
    if (!sessionId.value) {
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

async function pickDirAndUpload() {
    if (!sessionId.value) {
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
    await runTransfer(`${name}/ (目录)`, () =>
        remoteBackend.value.upload(dir, joinPath(remoteDir, name, '/')),
    )
    await remotePanel.value?.refresh()
}

// 拖到远程面板：操作系统文件（Wails 原生拖放） / 面板内部条目都走批量上传
async function onRemoteDrop(payload: DropPayload) {
    if (!sessionId.value) {
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
    } catch {
        /* ignore */
    }
    return null
}

async function downloadBatch(items: { path: string; name: string; isDir: boolean }[]) {
    if (!items.length) return
    if (!sessionId.value) {
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
            await remoteBackend.value.download(it.path, target, it.isDir)
            transfers.complete(sessionId.value, 'download', label)
            ok++
        } catch (e: any) {
            transfers.fail(sessionId.value, 'download', label, e?.message || String(e))
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

// 面板右键 / 双击动作
async function onPanelAction(payload: PanelAction) {
    switch (payload.action) {
        case 'pick-upload':
            await pickAndUpload()
            break
        case 'pick-upload-dir':
            await pickDirAndUpload()
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
        // 文档式编辑：双击文件 / 右键目录 → 跳到「远程编辑器」页打开（同独立 SFTP 页）
        // 板块记下来源标签（scope），切标签不会关闭它，只有该标签关闭 / 会话断开才关闭。
        case 'open-file':
            if (payload.entry) {
                openFileInEditor(remoteBackend.value, payload.entry, props.tabKey)
                void router.push('/remote-editor')
            }
            break
        case 'open-in-editor':
            if (payload.entry?.isDir) {
                openDirInEditor(remoteBackend.value, payload.entry.path, props.tabKey)
                void router.push('/remote-editor')
            }
            break
        case 'upload-entry':
        case 'upload-multi':
            break
    }
}
</script>

<style scoped>
.sftp-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 6px;
    padding: 8px;
}

.sftp-empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--text-secondary);
    font-size: 12.5px;
}

.sftp-empty p {
    margin: 0;
}

.sftp-head {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    min-height: 24px;
}

.sftp-err {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11.5px;
    color: #f56c6c;
}

.sftp-file-area {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.sftp-file-area :deep(.file-panel) {
    height: 100%;
}

.sftp-hint {
    margin-left: auto;
    font-size: 10.5px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
</style>
