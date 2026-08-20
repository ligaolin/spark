<template>
    <div class="file-panel" :class="{ 'drop-target': dragDepth > 0 }" @dragenter="onDragEnter" @dragover="onDragOver"
        @dragleave="onDragLeave" @drop="onDrop" @contextmenu.prevent="onBlankContext">
        <div class="panel-head">
            <span class="panel-title">{{ title }}</span>
            <span v-if="loading" class="panel-loading"><el-icon class="is-loading">
                    <Loading />
                </el-icon></span>
        </div>

        <div class="path-bar">
            <el-button size="small" text @click="goUp" :disabled="isRoot" title="上级目录">
                <el-icon>
                    <Top />
                </el-icon>
            </el-button>
            <el-button size="small" text @click="goHome" title="主目录">
                <el-icon>
                    <House />
                </el-icon>
            </el-button>
            <el-button size="small" text @click="refresh" title="刷新">
                <el-icon>
                    <Refresh />
                </el-icon>
            </el-button>
            <!-- 收藏：对应本面板（远程=服务器目录，本地=本机目录），与导航按钮同行 -->
            <el-popover v-if="favKey !== undefined" placement="bottom-start" :width="270" trigger="click">
                <template #reference>
                    <el-button size="small" text :title="props.backend.kind === 'local' ? '收藏的本地目录' : '收藏的远程目录'">
                        <el-icon class="fav-star" :class="{ active: favorites.length > 0 }">
                            <Star />
                        </el-icon>
                    </el-button>
                </template>
                <div class="fav-panel">
                    <div class="fav-head">
                        <span class="fav-title">
                            {{ props.backend.kind === 'local' ? '收藏的本地目录' : '收藏的远程目录' }}
                        </span>
                        <el-button size="small" text type="primary" @click="addFavorite">
                            ＋ 收藏当前目录
                        </el-button>
                    </div>
                    <div class="fav-list">
                        <div v-for="f in favorites" :key="f.id" class="fav-item">
                            <span class="fav-path" :title="f.path" @click="jumpFavorite(f)">{{ f.path }}</span>
                            <el-icon class="fav-del" @click="deleteFavorite(f)">
                                <Close />
                            </el-icon>
                        </div>
                        <div v-if="favorites.length === 0" class="fav-empty">
                            暂无收藏<br />点击"＋ 收藏当前目录"
                        </div>
                    </div>
                </div>
            </el-popover>
            <el-input v-model="pathInput" class="path-input" size="small" @keyup.enter="cd(pathInput)"
                @blur="pathInput = currentPath" :placeholder="placeholder" />
        </div>

        <div class="table-wrap file-table">
            <el-table :data="entries" height="100%" size="small" :highlight-current-row="!multiSelect"
                :row-class-name="rowClassName" @current-change="onSelect" @row-click="onRowClick"
                @row-dblclick="onDblClick" @row-contextmenu="onRowContext" empty-text="空目录">
                <el-table-column label="名称" min-width="180">
                    <template #default="{ row }">
                        <span class="entry-name" draggable="true" :title="dragHint"
                            @dragstart="onDragStart($event, row)">
                            <el-icon v-if="row.isDir" color="#e6c06c">
                                <Folder />
                            </el-icon>
                            <el-icon v-else-if="row.symlink" color="#7fb0ff">
                                <Link />
                            </el-icon>
                            <el-icon v-else color="#8b90a0">
                                <Document />
                            </el-icon>
                            <span class="name-text" :title="row.name">{{ row.name }}</span>
                            <el-tag v-if="row.symlink" size="small" type="info" class="link-tag">链接</el-tag>
                        </span>
                    </template>
                </el-table-column>
                <el-table-column label="大小" width="90" align="right">
                    <template #default="{ row }">
                        <span class="mono dim">{{ row.isDir ? '-' : formatSize(row.size) }}</span>
                    </template>
                </el-table-column>
                <el-table-column label="修改时间" width="130">
                    <template #default="{ row }">
                        <span class="dim">{{ formatTime(row.modTime) }}</span>
                    </template>
                </el-table-column>
                <el-table-column v-if="showMode" label="权限" width="110">
                    <template #default="{ row }">
                        <span class="mono dim">{{ row.mode }}</span>
                    </template>
                </el-table-column>
            </el-table>
        </div>

        <div class="panel-foot">
            <slot name="actions" :selected="selected" :selectedRows="selectedRows" :currentPath="currentPath" />
            <span v-if="multiSelect && selectedRows.length > 0" class="sel-info">
                已选 <b>{{ selectedRows.length }}</b> 项（点击可取消）
            </span>
            <span v-else-if="selected" class="sel-info">
                已选：<b>{{ selected.name }}</b>
            </span>
        </div>

        <ContextMenu v-model="ctxVisible" :x="ctxX" :y="ctxY" :items="ctxItems" @pick="onCtxPick" />
    </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
    Top,
    House,
    Refresh,
    Loading,
    Folder,
    Document,
    Link,
    FolderAdd,
    Upload,
    Download,
    Edit,
    Delete,
    Lock,
    FolderOpened,
    Star,
    Close,
} from '@element-plus/icons-vue'
import ContextMenu from './ContextMenu.vue'
import type { CtxItem } from './ContextMenu.vue'
import { FavoriteService, makeFavorite } from '../utils/wails'
import type { Favorite } from '../utils/wails'
import { showInputDialog, showConfirmDialog } from '../utils/dialog'
import type { DropPayload, FileEntry, PanelAction } from '../types'
import { formatSize, formatTime } from '../types'
import { parentDir, type FileBackend } from '../utils/fileBackend'

const props = defineProps<{
    backend: FileBackend
    title: string
    placeholder?: string
    showMode?: boolean
    // 是否已连接远程（未连接时禁用上传/下载等菜单项）
    connected?: boolean
    // 收藏键：本地面板传 0；远程面板传连接 ID（不传则显示收藏入口）
    favKey?: number
    // 启用复选框多选（用于批量上传）
    multiSelect?: boolean
}>()

const emit = defineEmits<{
    (e: 'drop', payload: DropPayload): void
    (e: 'action', payload: PanelAction): void
}>()

// 收藏（本面板对应：本地 = 本机目录，远程 = 服务器目录）
const favorites = ref<Favorite[]>([])

async function loadFavorites() {
    const key = props.favKey ?? 0
    const kind = props.backend.kind === 'local' ? 'local' : 'remote'
    if (kind === 'remote' && key === 0) {
        favorites.value = []
        return
    }
    try {
        favorites.value = (await FavoriteService.List(kind, key)) ?? []
    } catch (e: any) {
        ElMessage.error(`加载收藏失败：${e?.message || e}`)
    }
}

watch(
    () => [props.favKey, props.backend.kind] as const,
    () => loadFavorites(),
    { immediate: true },
)

async function addFavorite() {
    const key = props.favKey ?? 0
    const kind = props.backend.kind === 'local' ? 'local' : 'remote'
    if (kind === 'remote' && key === 0) {
        ElMessage.warning('请先选择连接')
        return
    }
    const path = currentPath.value
    if (!path) {
        ElMessage.warning('请先进入要收藏的目录')
        return
    }
    try {
        await FavoriteService.Create(makeFavorite({ kind, connectionId: key, path }))
        ElMessage.success(`已收藏 ${path}`)
        await loadFavorites()
    } catch (e: any) {
        ElMessage.error(e?.message || String(e))
    }
}

async function jumpFavorite(f: Favorite) {
    if (props.backend.kind === 'remote' && !props.connected) {
        ElMessage.warning('请先连接远程服务器')
        return
    }
    await cd(f.path)
}

async function deleteFavorite(f: Favorite) {
    const ok = await showConfirmDialog('删除收藏', `确定删除收藏「${f.path}」？`, true, '删除')
    if (!ok) return
    try {
        await FavoriteService.Delete(f.id)
        await loadFavorites()
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

const currentPath = ref('')
const pathInput = ref('')
const entries = ref<FileEntry[]>([])
const loading = ref(false)
const selected = ref<FileEntry | null>(null)
const selectedRows = ref<FileEntry[]>([])

// 拖拽高亮状态（用计数器避免子元素间移动时闪烁）
const dragDepth = ref(0)

const dragHint = computed(() =>
    props.backend.kind === 'local' ? '拖到右侧远程面板以上传' : '拖到左侧本地面板以下载',
)

// 本面板接受的数据类型
const mimeLocal = 'application/x-spark-local'
const mimeRemote = 'application/x-spark-remote'

function hasRelevantData(e: DragEvent): boolean {
    const types = e.dataTransfer?.types || []
    if (props.backend.kind === 'local') {
        return types.includes(mimeRemote)
    }
    // 远程面板：接受内部本地条目 + 操作系统文件
    return types.includes(mimeLocal) || types.includes('Files')
}

function onDragStart(e: DragEvent, row: FileEntry) {
    const mime = props.backend.kind === 'local' ? mimeLocal : mimeRemote
    // 拖拽的条目属于多选集合时，携带整个多选集合一起传输；否则只带当前条目
    let entries: { path: string; name: string; isDir: boolean }[]
    if (props.multiSelect && selectedRows.value.some((r) => r.path === row.path)) {
        entries = selectedRows.value.map((r) => ({ path: r.path, name: r.name, isDir: r.isDir }))
    } else {
        entries = [{ path: row.path, name: row.name, isDir: row.isDir }]
    }
    e.dataTransfer?.setData(mime, JSON.stringify(entries))
    if (e.dataTransfer) {
        e.dataTransfer.effectAllowed = 'copy'
    }
}

function onDragEnter(e: DragEvent) {
    if (hasRelevantData(e)) {
        dragDepth.value++
    }
}

function onDragOver(e: DragEvent) {
    if (hasRelevantData(e)) {
        e.preventDefault()
        if (e.dataTransfer) {
            e.dataTransfer.dropEffect = 'copy'
        }
    }
}

function onDragLeave() {
    dragDepth.value = Math.max(0, dragDepth.value - 1)
}

function onDrop(e: DragEvent) {
    dragDepth.value = 0
    if (!hasRelevantData(e)) return
    e.preventDefault()
    const dt = e.dataTransfer
    if (!dt) return

    if (props.backend.kind === 'local') {
        const raw = dt.getData(mimeRemote)
        if (raw) {
            try {
                emit('drop', { source: 'remote', entries: [JSON.parse(raw)] })
            } catch {
                /* ignore */
            }
        }
        return
    }

    // 远程面板
    const rawLocal = dt.getData(mimeLocal)
    if (rawLocal) {
        try {
            const parsed = JSON.parse(rawLocal)
            // 兼容单个条目与多选条目数组两种负载
            const list = Array.isArray(parsed) ? parsed : [parsed]
            emit('drop', { source: 'local', entries: list })
            return
        } catch {
            /* ignore */
        }
    }
    // 操作系统文件（资源管理器拖入）
    const paths: string[] = []
    const files = dt.files
    for (let i = 0; i < files.length; i++) {
        const f = files[i] as File & { path?: string }
        if (f.path) {
            paths.push(f.path)
        }
    }
    if (paths.length > 0) {
        emit('drop', { source: 'files', paths })
    } else if (files.length > 0) {
        ElMessage.warning('无法获取文件路径，请使用「上传」按钮选择文件')
    }
}

const isRoot = computed(() => {
    const p = currentPath.value
    if (!p) return true
    // 到达根目录时 parentDir 返回自身（如 /、C:/）
    return parentDir(p, props.backend.sep) === p
})

// 右键菜单状态
const ctxVisible = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)
const ctxItems = ref<(CtxItem | 'divider')[]>([])
const ctxEntry = ref<FileEntry | null>(null)

const linked = computed(() => props.connected !== false)

function onBlankContext(e: MouseEvent) {
    ctxEntry.value = null
    ctxItems.value = buildMenu(null)
    openCtx(e)
}

function onRowContext(row: FileEntry, _column: unknown, event: MouseEvent) {
    // 必须先 preventDefault：行右键这里会 stopPropagation，事件到不了
    // 面板/全局的 contextmenu 处理器，不拦截就会弹出系统菜单盖住我们的菜单
    event.preventDefault()
    event.stopPropagation() // 避免触发面板空白菜单
    selected.value = row
    ctxEntry.value = row
    ctxItems.value = buildMenu(row)
    openCtx(event)
}

function openCtx(e: MouseEvent) {
    ctxX.value = e.clientX
    ctxY.value = e.clientY
    ctxVisible.value = false
    // 下一帧再打开，确保位置更新
    requestAnimationFrame(() => {
        ctxVisible.value = true
    })
}

function buildMenu(entry: FileEntry | null): (CtxItem | 'divider')[] {
    const isLocal = props.backend.kind === 'local'
    if (!entry) {
        // 空白区域：操作当前目录
        return [
            { key: 'pick-upload', label: '上传文件…', icon: Upload, disabled: !linked.value },
            { key: 'pick-upload-dir', label: '上传目录…', icon: FolderAdd, disabled: !linked.value },
            'divider',
            { key: 'mkdir', label: '新建目录', icon: FolderAdd },
            { key: 'refresh', label: '刷新', icon: Refresh },
            { key: 'up', label: '返回上级', icon: Top, disabled: isRoot.value },
            { key: 'home', label: '主目录', icon: House },
        ]
    }
    // 文件/目录：操作条目
    const items: (CtxItem | 'divider')[] = []
    if (isLocal) {
        items.push({
            key: 'upload-entry',
            label: entry.isDir ? '上传此目录' : '上传此文件',
            icon: Upload,
            disabled: !linked.value,
        })
    } else {
        items.push({
            key: 'download-entry',
            label: entry.isDir ? '下载此目录' : '下载此文件',
            icon: Download,
            disabled: !linked.value,
        })
    }
    items.push(
        {
            key: 'open',
            label: entry.isDir ? '打开 / 进入' : '打开',
            icon: FolderOpened,
            disabled: !entry.isDir,
        },
        'divider',
        { key: 'rename', label: '重命名', icon: Edit },
    )
    if (!isLocal && props.backend.chmod) {
        items.push({ key: 'chmod', label: '修改权限', icon: Lock })
    }
    items.push({ key: 'remove', label: '删除', icon: Delete, danger: true })
    return items
}

async function onCtxPick(item: CtxItem) {
    const entry = ctxEntry.value
    switch (item.key) {
        case 'pick-upload':
            emit('action', { action: 'pick-upload' })
            break
        case 'pick-upload-dir':
            emit('action', { action: 'pick-upload-dir' })
            break
        case 'upload-entry':
            if (entry) emit('action', { action: 'upload-entry', entry: { path: entry.path, name: entry.name, isDir: entry.isDir } })
            break
        case 'download-entry':
            if (entry) emit('action', { action: 'download-entry', entry: { path: entry.path, name: entry.name, isDir: entry.isDir } })
            break
        case 'mkdir':
            await mkdir()
            break
        case 'refresh':
            await refresh()
            break
        case 'up':
            await goUp()
            break
        case 'home':
            await goHome()
            break
        case 'open':
            if (entry?.isDir) await cd(entry.path)
            break
        case 'rename':
            await rename()
            break
        case 'chmod':
            await chmod()
            break
        case 'remove':
            await remove()
            break
    }
}

async function cd(path: string) {
    if (!path) return
    loading.value = true
    try {
        const list = await props.backend.list(path)
        currentPath.value = path
        pathInput.value = path
        entries.value = list
        selected.value = null
        selectedRows.value = []
    } catch (e: any) {
        ElMessage.error(`打开目录失败：${e?.message || e}`)
    } finally {
        loading.value = false
    }
}

async function goUp() {
    if (isRoot.value) return
    await cd(parentDir(currentPath.value, props.backend.sep))
}

async function goHome() {
    try {
        const home = await props.backend.home()
        await cd(home || props.backend.sep)
    } catch (e: any) {
        ElMessage.error(`获取主目录失败：${e?.message || e}`)
    }
}

async function refresh() {
    await cd(currentPath.value)
}

function onSelect(row: FileEntry | null) {
    selected.value = row
}

// 多选模式：点击行切换 选中/取消 选中（不弹复选列）
function onRowClick(row: FileEntry) {
    if (!props.multiSelect) return
    selected.value = row
    const idx = selectedRows.value.findIndex((r) => r.path === row.path)
    if (idx >= 0) {
        selectedRows.value.splice(idx, 1)
    } else {
        selectedRows.value.push(row)
    }
}

// 多选高亮行样式
function rowClassName({ row }: { row: FileEntry }): string {
    if (props.multiSelect && selectedRows.value.some((r) => r.path === row.path)) {
        return 'is-multi-selected'
    }
    return ''
}

async function onDblClick(row: FileEntry) {
    if (row.isDir) {
        await cd(row.path)
    }
}

async function mkdir() {
    const values = await showInputDialog('新建目录', [{ key: 'name', label: '目录名称' }])
    if (!values) return
    const name = values.name.trim()
    if (!name) return
    const target = joinPathSafe(currentPath.value, name, props.backend.sep)
    try {
        await props.backend.mkdir(target)
        ElMessage.success('已创建')
        await refresh()
    } catch (e: any) {
        ElMessage.error(`创建失败：${e?.message || e}`)
    }
}

async function rename() {
    if (!selected.value) {
        ElMessage.warning('请先选择文件或目录')
        return
    }
    const values = await showInputDialog('重命名', [
        { key: 'name', label: '新名称', initial: selected.value.name },
    ])
    if (!values) return
    const name = values.name.trim()
    if (!name || name === selected.value.name) return
    const target = joinPathSafe(
        parentDir(selected.value.path, props.backend.sep),
        name,
        props.backend.sep,
    )
    try {
        await props.backend.rename(selected.value.path, target)
        ElMessage.success('已重命名')
        await refresh()
    } catch (e: any) {
        ElMessage.error(`重命名失败：${e?.message || e}`)
    }
}

async function remove() {
    if (!selected.value) {
        ElMessage.warning('请先选择文件或目录')
        return
    }
    const tip = selected.value.isDir
        ? `确定删除目录 ${selected.value.name} 及其全部内容？`
        : `确定删除文件 ${selected.value.name}？`
    const ok = await showConfirmDialog('删除', tip, true, '删除')
    if (!ok) return
    try {
        await props.backend.remove(selected.value.path, selected.value.isDir)
        ElMessage.success('已删除')
        await refresh()
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

async function chmod() {
    if (!selected.value) {
        ElMessage.warning('请先选择文件或目录')
        return
    }
    if (!props.backend.chmod) return
    const values = await showInputDialog('修改权限', [
        { key: 'mode', label: '八进制权限（如 755）', initial: '755' },
    ])
    if (!values) return
    const n = parseInt(values.mode.trim(), 8)
    if (isNaN(n)) {
        ElMessage.error('权限格式不正确')
        return
    }
    try {
        await props.backend.chmod!(selected.value.path, n)
        ElMessage.success('已修改')
        await refresh()
    } catch (e: any) {
        ElMessage.error(`修改权限失败：${e?.message || e}`)
    }
}

function joinPathSafe(base: string, name: string, sep: string): string {
    if (!base || base === sep) return `${sep}${name}`
    return `${base.replace(/\/+$/, '')}/${name}`
}

function clear() {
    entries.value = []
    selected.value = null
    selectedRows.value = []
    currentPath.value = ''
    pathInput.value = ''
}

defineExpose({
    currentPath,
    selected,
    selectedRows,
    cd,
    refresh,
    goHome,
    mkdir,
    rename,
    remove,
    chmod,
    clear,
})

// 本地面板挂载后自动进入主目录；远程面板由父组件在连接成功后触发 goHome
onMounted(() => {
    if (props.backend.kind === 'local') {
        goHome()
    }
})
</script>

<style scoped>
.file-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
    transition: box-shadow 0.15s, border-color 0.15s;
}

.file-panel.drop-target {
    border-color: var(--accent);
    box-shadow: inset 0 0 0 2px rgba(79, 140, 255, 0.45);
}

.panel-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.panel-title {
    font-size: 12.5px;
    font-weight: 600;
    letter-spacing: 0.5px;
    color: var(--text-primary);
}

.panel-loading {
    display: flex;
    align-items: center;
    color: var(--accent);
}

.fav-star {
    color: var(--text-secondary);
    font-size: 15px;
}

.fav-star.active {
    color: #e6c06c;
}

.fav-panel {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.fav-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    border-bottom: 1px solid var(--border-color);
    padding-bottom: 6px;
}

.fav-title {
    font-size: 12.5px;
    font-weight: 600;
}

.fav-list {
    max-height: 240px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
}

.fav-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    border-radius: 5px;
    cursor: pointer;
}

.fav-item:hover {
    background: #2e3442;
}

.fav-path {
    flex: 1;
    font-family: var(--term-font);
    font-size: 12px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.fav-del {
    color: var(--text-secondary);
    border-radius: 4px;
    padding: 2px;
    flex-shrink: 0;
}

.fav-del:hover {
    color: #f56c6c;
    background: rgba(245, 108, 108, 0.12);
}

.fav-empty {
    font-size: 12px;
    color: var(--text-secondary);
    text-align: center;
    padding: 14px 0;
    line-height: 1.7;
}

.path-bar {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 4px 8px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.table-wrap {
    flex: 1;
    min-height: 0;
}

.entry-name {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    cursor: grab;
    user-select: none;
}

.name-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.link-tag {
    transform: scale(0.8);
    margin-left: 2px;
}

.dim {
    color: var(--text-secondary);
    font-size: 12px;
}

.panel-foot {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    border-top: 1px solid var(--border-color);
    flex-shrink: 0;
    min-height: 36px;
}

.sel-info {
    margin-left: auto;
    font-size: 12px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
</style>
