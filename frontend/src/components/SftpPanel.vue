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
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Events } from '@wailsio/runtime'
import { FolderOpened } from '@element-plus/icons-vue'
import FilePanel from './FilePanel.vue'
import TransferDock from './TransferDock.vue'
import { useTransfersStore } from '../stores/transfers'
import { openDirInEditor, openFileInEditor, closeAllPanels } from '../stores/remoteEditor'
import { SFTPFileService, EVENTS, LocalService } from '../utils/wails'
import type { ConnectOptions } from '../utils/wails'
import type { DropPayload, PanelAction } from '../types'
import { joinPath, makeSftpBackend, type FileBackend } from '../utils/fileBackend'
import { resolveHostKeyIssue } from '../utils/hostkey'
import { te } from 'element-plus/es/locale/index.mjs'

const props = defineProps<{
    // 当前活动 SSH 标签的连接参数；为空（无标签）时断开并显示引导
    opts: ConnectOptions | null
    // 目录收藏键（来源已保存连接 id；快速连接为 0 时不展示收藏）
    favKey?: number
}>()

const router = useRouter()
const transfers = useTransfersStore()
const remotePanel = ref<InstanceType<typeof FilePanel>>()

const sessionId = ref('')
const error = ref('')
const connected = computed(() => !!sessionId.value)

// 远程后端：方法在调用时读取 sessionId.value（getter 形式），
// 连接成功后再调用 goHome 等也能拿到最新会话 id，避免旧后端对象竞态
const rawBackend = makeSftpBackend(() => sessionId.value)

// ---------- 会话丢失自动重连 ----------
// 移动端网络不稳时 SFTP 会话可能被保活判定死亡而清理，但前端可能尚未
// 收到 session:closed；在「会话不存在/已关闭」类错误时自动重连并重试一次。

function isSessionGone(e: any): boolean {
    const m = String(e?.message || e)
    return m.includes('会话') && (m.includes('不存在') || m.includes('已关闭'))
}

let reconnectPromise: Promise<boolean> | null = null
let reconnectingNow = false
async function reconnectAndWait(): Promise<boolean> {
    if (!props.opts) return false
    if (reconnectPromise) {
        // 就是本次重连内部（如 connect 的 goHome）再次触发时直接放弃，
        // 避免自等待死锁。
        if (reconnectingNow) return false
        return reconnectPromise
    }
    if (sessionId.value) {
        SFTPFileService.Disconnect(sessionId.value).catch(() => undefined)
        sessionId.value = ''
    }
    remotePanel.value?.clear()
    reconnectingNow = true
    reconnectPromise = connect()
        .then(() => !!sessionId.value)
        .finally(() => {
            reconnectPromise = null
            reconnectingNow = false
        })
    return reconnectPromise
}

function withReconnect(fn: (...args: any[]) => Promise<any>) {
    return async (...args: any[]) => {
        try {
            return await fn(...args)
        } catch (e) {
            if (isSessionGone(e) && (await reconnectAndWait())) {
                return await fn(...args)
            }
            throw e
        }
    }
}

const remoteBackend: FileBackend = {
    ...rawBackend,
    home: withReconnect(rawBackend.home),
    list: withReconnect(rawBackend.list),
    mkdir: withReconnect(rawBackend.mkdir),
    rename: withReconnect(rawBackend.rename),
    remove: withReconnect(rawBackend.remove),
    chmod: withReconnect(rawBackend.chmod!),
    upload: withReconnect(rawBackend.upload),
    download: withReconnect(rawBackend.download),
    readFile: withReconnect(rawBackend.readFile),
    writeFile: withReconnect(rawBackend.writeFile),
    search: withReconnect(rawBackend.search),
    replace: withReconnect(rawBackend.replace),
}

function basename(p: string): string {
    const parts = p.split(/[\\/]/)
    return parts[parts.length - 1] || p
}

// ---------- 连接生命周期 ----------

// 连接序号：每次连接请求递增，用于丢弃「过期」的在途连接结果，
// 避免切标签/重连时旧的连接回写覆盖新会话（竞态导致"会话不存在"）。
let connectSeq = 0

async function connect(hostKeyRetry = 0) {
    const opts = props.opts
    if (!opts) return
    const seq = ++connectSeq
    // connecting.value = true
    error.value = ''
    try {
        const id = await SFTPFileService.Connect(opts)
        if (seq !== connectSeq) {
            // 期间有更新的连接请求，本次结果作废并清理掉这个多余会话
            SFTPFileService.Disconnect(id).catch(() => undefined)
            return
        }
        sessionId.value = id
        await remotePanel.value?.goHome()
    } catch (e: any) {
        if (seq !== connectSeq) return
        if (hostKeyRetry === 0) {
            const accepted = await resolveHostKeyIssue(e, opts)
            if (accepted) {
                await connect(1)
                return
            }
        }
        sessionId.value = ''
        error.value = e?.message || String(e)
    }
}

function disconnect() {
    connectSeq++ // 使在途连接失效
    const id = sessionId.value
    sessionId.value = ''
    if (id) {
        SFTPFileService.Disconnect(id).catch(() => undefined)
    }
    remotePanel.value?.clear()
    // 会话断开后其编辑器板块不再可用，全部关闭
    closeAllPanels()
}

function reconnect() {
    void connect()
}

// 活动标签切换 → 切换 SFTP 连接；无标签 → 断开。
// immediate：首次挂载时若已有活动标签（如终端页 keep-alive 恢复）也立即连接
watch(
    () => props.opts,
    (opts, prev) => {
        if (prev) disconnect()
        if (opts) void connect()
    },
    { immediate: true },
)

// 保活检测到连接断开时清理会话状态
let unSessionClosed: (() => void) | null = null
unSessionClosed = Events.On(EVENTS.sessionClosed, (evt: any) => {
    const sc = evt.data
    if (sc && sc.sessionId === sessionId.value) {
        sessionId.value = ''
        remotePanel.value?.clear()
        closeAllPanels()
        error.value = sc.reason || '连接已断开，请重新连接'
    }
})

onBeforeUnmount(() => {
    unSessionClosed?.()
    disconnect()
})

transfers.bind()

// ---------- 上传 ----------

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
            await remoteBackend.upload(it.path, target)
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
        remoteBackend.upload(dir, joinPath(remoteDir, name, '/')),
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
            await remoteBackend.download(it.path, target, it.isDir)
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
        case 'open-file':
            if (payload.entry) {
                openFileInEditor(remoteBackend, payload.entry)
                void router.push('/remote-editor')
            }
            break
        case 'open-in-editor':
            if (payload.entry?.isDir) {
                openDirInEditor(remoteBackend, payload.entry.path)
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
