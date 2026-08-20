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
            <el-button size="small" text @click="openSearch" title="搜索（文件名 / 文件内容）">
                <el-icon>
                    <Search />
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

        <div ref="tableWrapRef" class="table-wrap file-table" @mousedown="onTableMouseDown">
            <el-table :data="entries" height="100%" size="small" :highlight-current-row="!multiSelect"
                :row-class-name="rowClassName" @current-change="onSelect" @row-click="onRowClick"
                @row-dblclick="onDblClick" @row-contextmenu="onRowContext" empty-text="空目录">
                <el-table-column label="名称" min-width="180">
                    <template #default="{ row }">
                        <span class="entry-name" draggable="true" :title="dragHint"
                            @dragstart="onDragStart($event, row)"
                            @dragover="onRowDragOver($event, row)"
                            @dragleave="onRowDragLeave($event, row)"
                            @drop="onRowDrop($event, row)">
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
            <!-- 框选矩形覆盖层 -->
            <div v-if="boxSelecting" class="box-select-overlay" :style="boxStyle"></div>
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

        <!-- 文本编辑器：双击文件打开 -->
        <TextEditor ref="editorRef" />
        <!-- 搜索：文件名 / 文件内容，递归搜索 -->
        <SearchDialog ref="searchRef" :backend="backend" @pick="onSearchPick" />
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
    Search,
} from '@element-plus/icons-vue'
import ContextMenu from './ContextMenu.vue'
import type { CtxItem } from './ContextMenu.vue'
import TextEditor from './TextEditor.vue'
import SearchDialog from './SearchDialog.vue'
import { FavoriteService, makeFavorite } from '../utils/wails'
import type { Favorite } from '../utils/wails'
import { showInputDialog, showConfirmDialog } from '../utils/dialog'
import type { DropPayload, FileEntry, PanelAction, SearchResult } from '../types'
import { formatSize, formatTime } from '../types'
import { joinPath, parentDir, type FileBackend } from '../utils/fileBackend'

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

// 编辑器 / 搜索
const editorRef = ref<InstanceType<typeof TextEditor>>()
const searchRef = ref<InstanceType<typeof SearchDialog>>()

function openEditor(file: { path: string; name: string }, lineNo?: number) {
  editorRef.value?.open(props.backend, file, lineNo)
}

async function openSearch() {
  if (props.backend.kind === 'remote' && props.connected === false) {
    ElMessage.warning('请先连接远程服务器')
    return
  }
  let dir = currentPath.value
  if (!dir) {
    try {
      dir = await props.backend.home()
    } catch {
      /* ignore */
    }
  }
  if (!dir) {
    ElMessage.warning('请先进入目录')
    return
  }
  searchRef.value?.open(dir)
}

function onSearchPick(r: SearchResult) {
  if (r.isDir) {
    void cd(r.path)
  } else {
    openEditor({ path: r.path, name: r.name }, r.lineNo)
  }
}

// 拖拽高亮状态（用计数器避免子元素间移动时闪烁）
const dragDepth = ref(0)
// 当前悬停的可放置目录路径（用于高亮该目录行）
const dropTargetPath = ref('')

// 框选（拉框多选）状态
const tableWrapRef = ref<HTMLElement>()
const boxSelecting = ref(false)
const boxStart = ref({ x: 0, y: 0 })
const boxCurrent = ref({ x: 0, y: 0 })
// 框选预览结果（非响应式，仅在 mouseup 提交时转为 selectedRows）
let boxPreview: FileEntry[] = []
let boxAdditive = false
let boxRaf = 0

const boxStyle = computed(() => {
    const x1 = boxStart.value.x
    const y1 = boxStart.value.y
    const x2 = boxCurrent.value.x
    const y2 = boxCurrent.value.y
    return {
        left: `${Math.min(x1, x2)}px`,
        top: `${Math.min(y1, y2)}px`,
        width: `${Math.abs(x2 - x1)}px`,
        height: `${Math.abs(y2 - y1)}px`,
    }
})

const dragHint = computed(() =>
    props.backend.kind === 'local'
        ? '拖到右侧远程面板上传，或拖到本面板目录内移动'
        : '拖到左侧本地面板下载，或拖到本面板目录内移动',
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
        // 允许复制（跨面板传输）与移动（同面板拖入目录）两种放置效果
        e.dataTransfer.effectAllowed = 'copyMove'
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

// 解析拖拽负载中的条目数组（内部面板条目均以数组序列化）
function readEntries(dt: DataTransfer, mime: string): { path: string; name: string; isDir: boolean }[] {
    const raw = dt.getData(mime)
    if (!raw) return []
    try {
        const parsed = JSON.parse(raw)
        return Array.isArray(parsed) ? parsed : [parsed]
    } catch {
        return []
    }
}

function onDrop(e: DragEvent) {
    dragDepth.value = 0
    handleDrop(e, undefined)
}

// 目录行上的拖拽：判断是同面板移动还是跨面板传输
function rowRelevantData(e: DragEvent): 'move' | 'copy' | null {
    const types = e.dataTransfer?.types || []
    const own = props.backend.kind === 'local' ? mimeLocal : mimeRemote
    const other = props.backend.kind === 'local' ? mimeRemote : mimeLocal
    if (types.includes(own)) return 'move'
    if (props.backend.kind === 'local') {
        return types.includes(other) ? 'copy' : null
    }
    return types.includes(other) || types.includes('Files') ? 'copy' : null
}

function onRowDragOver(e: DragEvent, row: FileEntry) {
    if (!row.isDir) return
    const kind = rowRelevantData(e)
    if (!kind) return
    e.preventDefault()
    e.stopPropagation()
    if (e.dataTransfer) {
        e.dataTransfer.dropEffect = kind === 'move' ? 'move' : 'copy'
    }
    dropTargetPath.value = row.path
}

function onRowDragLeave(e: DragEvent, row: FileEntry) {
    if (dropTargetPath.value === row.path) {
        dropTargetPath.value = ''
    }
}

function onRowDrop(e: DragEvent, row: FileEntry) {
    if (!row.isDir) return
    const kind = rowRelevantData(e)
    if (!kind) return
    e.preventDefault()
    e.stopPropagation()
    dragDepth.value = 0
    dropTargetPath.value = ''
    if (kind === 'move') {
        void moveToDir(e, row.path)
    } else {
        handleDrop(e, row.path)
    }
}

// 归一化路径用于比较（折叠分隔符、去末尾分隔符，统一为 /）
function normPath(p: string): string {
    return p.replace(/[\\/]+$/, '').replace(/[\\/]/g, '/')
}

// 判断 target 是否等于 base 或位于 base 之下（分隔符无关，兼容 Windows/POSIX）
function isDescendantOrSelf(target: string, base: string): boolean {
    const t = normPath(target)
    const b = normPath(base)
    if (t === b) return true
    return t.startsWith(b + '/')
}

// 同面板移动：把条目拖到某个目录内（改名/移动）
async function moveToDir(e: DragEvent, targetDir: string) {
    const dt = e.dataTransfer
    if (!dt) return
    const mime = props.backend.kind === 'local' ? mimeLocal : mimeRemote
    const entries = readEntries(dt, mime)
    if (!entries.length) return
    const sep = props.backend.sep
    // 过滤非法移动：
    //  - 目录不能移动到其自身或其子目录内
    //  - 已在目标目录内的条目跳过（无操作）
    const valid: { path: string; name: string; target: string }[] = []
    let skipped = 0
    for (const en of entries) {
        if (en.isDir && isDescendantOrSelf(targetDir, en.path)) {
            skipped++
            continue
        }
        const target = joinPath(targetDir, en.name, sep)
        if (normPath(target) === normPath(en.path)) {
            skipped++
            continue
        }
        valid.push({ path: en.path, name: en.name, target })
    }
    if (valid.length === 0) {
        ElMessage.warning('所选条目已在目标目录中，或不能移动到自身/子目录')
        return
    }
    let ok = 0
    const failed: string[] = []
    for (const en of valid) {
        try {
            await props.backend.rename(en.path, en.target)
            ok++
        } catch (err: any) {
            failed.push(en.name)
            ElMessage.error(`移动 ${en.name} 失败：${err?.message || err}`)
        }
    }
    if (failed.length === 0) {
        ElMessage.success(skipped ? `已移动 ${ok} 项（跳过 ${skipped} 项）` : `已移动 ${ok} 项到 ${targetDir}`)
    } else {
        ElMessage.error(`移动完成：成功 ${ok} 项，失败 ${failed.length} 项（${failed.slice(0, 3).join('、')}）`)
    }
    await refresh()
}

// 统一的跨面板放置处理；targetDir 缺省为面板当前目录
function handleDrop(e: DragEvent, targetDir?: string) {
    if (!hasRelevantData(e)) return
    e.preventDefault()
    const dt = e.dataTransfer
    if (!dt) return

    if (props.backend.kind === 'local') {
        const entries = readEntries(dt, mimeRemote)
        if (entries.length) {
            emit('drop', { source: 'remote', entries, targetDir })
        }
        return
    }

    // 远程面板
    const localEntries = readEntries(dt, mimeLocal)
    if (localEntries.length) {
        emit('drop', { source: 'local', entries: localEntries, targetDir })
        return
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
        emit('drop', { source: 'files', paths, targetDir })
    } else if (files.length > 0) {
        ElMessage.warning('无法获取文件路径，请使用「上传」按钮选择文件')
    }
}

// ---------- 框选（拉框多选） ----------

function selectionRect() {
    const x1 = boxStart.value.x
    const y1 = boxStart.value.y
    const x2 = boxCurrent.value.x
    const y2 = boxCurrent.value.y
    return {
        left: Math.min(x1, x2),
        right: Math.max(x1, x2),
        top: Math.min(y1, y2),
        bottom: Math.max(y1, y2),
    }
}

function intersects(
    r: { left: number; right: number; top: number; bottom: number },
    s: { left: number; right: number; top: number; bottom: number },
): boolean {
    return !(r.right < s.left || r.left > s.right || r.bottom < s.top || r.top > s.bottom)
}

// 从空白区域按下左键启动框选（点在行/表头/滚动条上时不启动）
function onTableMouseDown(e: MouseEvent) {
    if (!props.multiSelect) return
    if (e.button !== 0) return
    const target = e.target as HTMLElement
    if (target.closest('tr.el-table__row')) return
    if (target.closest('.el-table__header-wrapper')) return
    if (target.closest('.el-scrollbar__bar')) return
    const wrap = tableWrapRef.value
    if (!wrap) return

    const rect = wrap.getBoundingClientRect()
    boxStart.value = { x: e.clientX - rect.left, y: e.clientY - rect.top }
    boxCurrent.value = { x: e.clientX - rect.left, y: e.clientY - rect.top }
    boxSelecting.value = true
    boxAdditive = e.ctrlKey || e.metaKey
    boxPreview = []
    e.preventDefault()
    document.body.style.userSelect = 'none'
    window.addEventListener('mousemove', onBoxMove)
    window.addEventListener('mouseup', onBoxUp)
}

function onBoxMove(e: MouseEvent) {
    if (!boxSelecting.value) return
    const wrap = tableWrapRef.value
    if (!wrap) return
    const rect = wrap.getBoundingClientRect()
    boxCurrent.value = { x: e.clientX - rect.left, y: e.clientY - rect.top }
    if (boxRaf) return
    boxRaf = requestAnimationFrame(() => {
        boxRaf = 0
        updateBoxPreview()
    })
}

function updateBoxPreview() {
    const wrap = tableWrapRef.value
    if (!wrap) return
    const rows = Array.from(wrap.querySelectorAll<HTMLElement>('tr.el-table__row'))
    const wrapRect = wrap.getBoundingClientRect()
    const sel = selectionRect()
    const preview: FileEntry[] = []
    rows.forEach((rowEl, i) => {
        const r = rowEl.getBoundingClientRect()
        const rowBox = {
            left: r.left - wrapRect.left,
            right: r.right - wrapRect.left,
            top: r.top - wrapRect.top,
            bottom: r.bottom - wrapRect.top,
        }
        const hit = intersects(rowBox, sel)
        rowEl.classList.toggle('is-box-preview', hit)
        if (hit && entries.value[i]) preview.push(entries.value[i])
    })
    boxPreview = preview
}

function onBoxUp() {
    if (!boxSelecting.value) return
    boxSelecting.value = false
    window.removeEventListener('mousemove', onBoxMove)
    window.removeEventListener('mouseup', onBoxUp)
    document.body.style.userSelect = ''
    if (boxRaf) {
        cancelAnimationFrame(boxRaf)
        boxRaf = 0
    }
    updateBoxPreview() // 用最终框选范围刷新一次
    commitBoxSelection()
}

function commitBoxSelection() {
    const wrap = tableWrapRef.value
    if (wrap) {
        wrap.querySelectorAll<HTMLElement>('tr.el-table__row.is-box-preview').forEach((el) => {
            el.classList.remove('is-box-preview')
        })
    }
    const boxed = boxPreview
    boxPreview = []
    if (boxed.length === 0) {
        // 点击空白处或框选区域未覆盖任何条目：清空多选（Ctrl 按住则保持不变）
        if (!boxAdditive) {
            selectedRows.value = []
            selected.value = null
        }
        return
    }

    if (boxAdditive) {
        // Ctrl/Cmd 框选：并入现有选择
        const map = new Map(selectedRows.value.map((r) => [r.path, r]))
        for (const it of boxed) {
            if (!map.has(it.path)) map.set(it.path, it)
        }
        selectedRows.value = Array.from(map.values())
    } else {
        selectedRows.value = boxed
    }
    // 同步单选引用（父组件在无多选时回退使用 selected）
    selected.value = boxed[boxed.length - 1] ?? null
}

function clearBoxSelect() {
    if (boxSelecting.value) {
        boxSelecting.value = false
        window.removeEventListener('mousemove', onBoxMove)
        window.removeEventListener('mouseup', onBoxUp)
        document.body.style.userSelect = ''
    }
    const wrap = tableWrapRef.value
    if (wrap) {
        wrap.querySelectorAll<HTMLElement>('tr.el-table__row.is-box-preview').forEach((el) => {
            el.classList.remove('is-box-preview')
        })
    }
    boxPreview = []
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
    // 多选：右键已选中项保留多选；右键未选中项则切换为单选该项
    if (props.multiSelect) {
        if (!selectedRows.value.some((r) => r.path === row.path)) {
            selectedRows.value = [row]
        }
    }
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
    // 是否处于多选上下文：右键选中项之一，且选中数量 > 1
    const multi =
        props.multiSelect &&
        selectedRows.value.length > 1 &&
        selectedRows.value.some((r) => r.path === entry.path)

    if (multi) {
        const n = selectedRows.value.length
        return [
            isLocal
                ? { key: 'upload-multi', label: `上传选中（${n}）`, icon: Upload, disabled: !linked.value }
                : { key: 'download-multi', label: `下载选中（${n}）`, icon: Download, disabled: !linked.value },
            { key: 'remove', label: `删除选中（${n}）`, icon: Delete, danger: true },
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
            label: entry.isDir ? '打开 / 进入' : '打开（编辑）',
            icon: FolderOpened,
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
        case 'upload-multi':
            emit('action', { action: 'upload-multi' })
            break
        case 'download-multi':
            emit('action', { action: 'download-multi' })
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
            else if (entry) openEditor(entry)
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

// 多选高亮行样式 + 拖放目标目录高亮
function rowClassName({ row }: { row: FileEntry }): string {
    const classes: string[] = []
    if (props.multiSelect && selectedRows.value.some((r) => r.path === row.path)) {
        classes.push('is-multi-selected')
    }
    if (dropTargetPath.value && row.path === dropTargetPath.value) {
        classes.push('is-drop-target')
    }
    return classes.join(' ')
}

async function onDblClick(row: FileEntry) {
    if (row.isDir) {
        await cd(row.path)
    } else {
        openEditor(row)
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
    // 多选模式且选中多项时批量删除；否则删除单选
    const targets =
        props.multiSelect && selectedRows.value.length > 0
            ? selectedRows.value.slice()
            : selected.value
              ? [selected.value]
              : []
    if (targets.length === 0) {
        ElMessage.warning('请先选择文件或目录')
        return
    }
    const tip =
        targets.length === 1
            ? targets[0].isDir
                ? `确定删除目录 ${targets[0].name} 及其全部内容？`
                : `确定删除文件 ${targets[0].name}？`
            : `确定删除选中的 ${targets.length} 个项目？`
    const ok = await showConfirmDialog('删除', tip, true, '删除')
    if (!ok) return
    let okCount = 0
    const failed: string[] = []
    for (const t of targets) {
        try {
            await props.backend.remove(t.path, t.isDir)
            okCount++
        } catch (e: any) {
            failed.push(t.name)
            ElMessage.error(`删除 ${t.name} 失败：${e?.message || e}`)
        }
    }
    if (failed.length === 0) {
        ElMessage.success(`已删除 ${okCount} 项`)
    } else {
        ElMessage.error(`删除完成：成功 ${okCount} 项，失败 ${failed.length} 项`)
    }
    await refresh()
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

onBeforeUnmount(() => {
    clearBoxSelect()
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
    position: relative;
}

.box-select-overlay {
    position: absolute;
    z-index: 5;
    background: rgba(79, 140, 255, 0.12);
    border: 1px solid #5b9dff;
    pointer-events: none;
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
