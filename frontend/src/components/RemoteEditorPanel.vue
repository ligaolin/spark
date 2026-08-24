<template>
    <div class="remote-editor-panel">
        <el-splitter>
            <!-- 左侧：远程目录树（多级懒加载） -->
            <el-splitter-panel size="28%" :min="180">
                <div class="rep-tree">
                    <div class="rep-tree-head">
                        <span class="rep-root" :title="rootPath">{{ rootPath }}</span>
                        <el-button size="small" text title="新建文件" @click="createFile">
                            <el-icon><DocumentAdd /></el-icon>
                        </el-button>
                        <el-button size="small" text title="新建目录" @click="mkdir">
                            <el-icon><FolderAdd /></el-icon>
                        </el-button>
                        <!-- <el-button size="small" text title="重命名" :disabled="!canOperateSelected" @click="renameNode">
                            <el-icon><Edit /></el-icon>
                        </el-button>
                        <el-button size="small" text title="删除" :disabled="!canOperateSelected" @click="removeNode">
                            <el-icon><Delete /></el-icon>
                        </el-button>
                        <el-button v-if="canChmod" size="small" text title="修改权限" :disabled="!canOperateSelected" @click="chmodNode">
                            <el-icon><Lock /></el-icon>
                        </el-button> -->
                        <el-button size="small" text title="刷新当前目录" @click="refreshCurrent">
                            <el-icon><Refresh /></el-icon>
                        </el-button>
                        <el-button size="small" text :class="{ active: showSearch }"
                            :title="showSearch ? '切回目录树' : '搜索（文件名 / 文件内容）'" @click="toggleSearch">
                            <el-icon><Search /></el-icon>
                        </el-button>
                    </div>
                    <div v-show="showSearch" class="rep-tree-body rep-search-body">
                        <SearchPanel ref="searchRef" :backend="backend" @pick="onSearchPick" />
                    </div>
                    <div v-show="!showSearch" class="rep-tree-body" @contextmenu.prevent="onBlankContextMenu">
                        <el-tree
                            :key="treeVersion"
                            ref="treeRef"
                            :data="[]"
                            :props="{ label: 'name', children: 'children', isLeaf: 'isLeaf' }"
                            node-key="path"
                            lazy
                            :load="loadNode"
                            highlight-current
                            :expand-on-click-node="true"
                            draggable
                            :allow-drop="allowDrop"
                            @node-click="onNodeClick"
                            @node-contextmenu="onNodeContextMenu"
                            @node-drop="onNodeDrop"
                        >
                            <template #default="{ data }">
                                <span class="rep-tree-node">
                                    <el-icon :color="data.isDir ? '#e6c06c' : 'var(--text-secondary)'">
                                        <Folder v-if="data.isDir" />
                                        <Document v-else />
                                    </el-icon>
                                    <span class="rep-tree-name" :title="data.name">{{ data.name }}</span>
                                </span>
                            </template>
                        </el-tree>
                    </div>
                </div>
            </el-splitter-panel>

            <!-- 右侧：标签页编辑区（可同时打开多个文件） -->
            <el-splitter-panel :min="200">
                <div class="rep-editor">
                    <div class="rep-tabs">
                        <div v-for="f in openFiles" :key="f.key" class="rep-tab"
                            :class="{ active: f.key === activeKey }" @click="activeKey = f.key"
                            @contextmenu.prevent="onTabContext($event, f)">
                            <span class="rep-tab-title" :title="f.path">{{ f.name }}</span>
                            <span v-if="f.dirty" class="rep-dirty" title="未保存">●</span>
                            <el-icon class="rep-tab-close" title="关闭" @click.stop="closeFile(f)">
                                <Close />
                            </el-icon>
                        </div>
                        <div class="rep-tab-actions">
                            <el-button size="small" type="primary" :disabled="!activeFile" :loading="saving"
                                @click="saveActive">
                                保存
                            </el-button>
                        </div>
                    </div>

                    <div class="rep-editor-area">
                        <div v-for="f in openFiles" v-show="f.key === activeKey" :key="f.key" class="rep-pane">
                            <CodeEditor v-if="f.kind !== 'md'" :filename="f.name" :wrap="settings.editorWordWrap"
                                :ref="(el: any) => setEditorRef(f.key, el)" @change="(v: string) => onFileChange(f, v)"
                                @save="saveActive" />
                            <MarkdownEditor v-else :ref="(el: any) => setEditorRef(f.key, el)"
                                @change="(v: string) => onFileChange(f, v)" @save="saveActive" />
                        </div>
                        <div v-if="openFiles.length === 0" class="rep-empty">
                            <el-icon :size="34"><Document /></el-icon>
                            <p>在左侧目录中选择文件，双击或点击打开编辑</p>
                            <p class="sub">支持同时打开多个文件、Ctrl+S 保存</p>
                        </div>
                        <div v-if="loadingFile" class="rep-loading">
                            <el-icon class="is-loading"><Loading /></el-icon>
                            <span>正在读取文件…</span>
                        </div>
                    </div>
                </div>
            </el-splitter-panel>
        </el-splitter>

        <ContextMenu v-model="ctxVisible" :x="ctxX" :y="ctxY" :items="ctxItems" @pick="onCtxPick" />
        <ContextMenu v-model="tabCtxVisible" :x="tabCtxX" :y="tabCtxY" :items="tabCtxItems" @pick="onTabCtxPick" />
    </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
    Folder,
    Document,
    Close,
    Refresh,
    Loading,
    DocumentAdd,
    FolderAdd,
    Edit,
    Delete,
    Lock,
    CloseBold,
    DArrowRight,
    CircleClose,
    Search,
    Upload,
    Download,
} from '@element-plus/icons-vue'
import CodeEditor from './CodeEditor.vue'
import MarkdownEditor from './MarkdownEditor.vue'
import ContextMenu from './ContextMenu.vue'
import type { CtxItem } from './ContextMenu.vue'
import SearchPanel from './SearchPanel.vue'
import { showConfirmDialog, showInputDialog } from '../utils/dialog'
import { useSettingsStore } from '../stores/settings'
import { LocalService } from '../utils/wails'
import type { SearchResult } from '../types'
import { parentDir, joinPath, type FileBackend } from '../utils/fileBackend'

// 两个编辑器组件暴露同一套操作方法
interface EditorApi {
    setContent(v: string): void
    getContent(): string
    focus(): void
}

interface OpenFile {
    key: string
    path: string
    name: string
    kind: 'text' | 'md'
    original: string
    dirty: boolean
}

const props = defineProps<{
    backend: FileBackend
    rootPath: string
}>()

const settings = useSettingsStore()

const treeRef = ref()
const treeVersion = ref(0)
const searchRef = ref<InstanceType<typeof SearchPanel>>()
const showSearch = ref(false)
const openFiles = ref<OpenFile[]>([])
const activeKey = ref<string | null>(null)
const saving = ref(false)
const loadingFile = ref(false)

const editorRefs = ref<Record<string, EditorApi | null>>({})

const activeFile = computed(() => openFiles.value.find((f) => f.key === activeKey.value) ?? null)

// ---------- 目录树操作（新建文件 / 新建目录 / 重命名 / 删除 / 改权限 / 刷新） ----------

interface TreeNode {
    path: string
    name: string
    isDir: boolean
}

// 当前选中的树节点；未选中时操作针对根目录
const selectedNode = ref<TreeNode | null>(null)
// 右键菜单状态
const ctxVisible = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)
const ctxItems = ref<(CtxItem | 'divider')[]>([])

// 文件标签页右键菜单
const tabCtxVisible = ref(false)
const tabCtxX = ref(0)
const tabCtxY = ref(0)
const tabCtxItems = ref<(CtxItem | 'divider')[]>([])
const tabCtxFile = ref<OpenFile | null>(null)
const tabCtxIndex = ref(-1)

// FTP 后端没有 chmod 能力（仅 SFTP 提供）
const canChmod = computed(() => !!props.backend.chmod)
// 上传 / 下载仅对远程后端有意义（本地文件夹无需）
const isRemote = computed(() => props.backend.kind === 'remote')
// 重命名 / 删除 / 改权限：需要一个非根节点的选中项
const canOperateSelected = computed(
    () => !!selectedNode.value && selectedNode.value.path !== props.rootPath,
)
// 新建文件 / 新建目录的落点：选中目录 → 该目录；选中文件 → 其父目录；未选中 → 根目录
const currentDir = computed(() => {
    const n = selectedNode.value
    if (!n) return props.rootPath
    return n.isDir ? n.path : parentDir(n.path, props.backend.sep)
})

function setEditorRef(key: string, el: EditorApi | null) {
    editorRefs.value[key] = el
}

function basename(p: string): string {
    // 兼容 POSIX（/）与 Windows（\）路径（本地文件夹与远程目录共用此面板）
    const parts = p.split(/[\\/]/)
    return parts[parts.length - 1] || p
}

// ---------- 目录树（懒加载） ----------
// 注意：el-tree 懒加载模式初始化时会以 level 0 的根节点调用 load（此时
// node.data 是 :data 数组，不是单个节点），必须在这里先解析出根目录节点，
// 否则会拿 undefined 路径去列目录（SFTP 会列成用户目录、FTP 列成根目录）。

async function loadNode(node: any, resolve: (children: any[]) => void) {
    // level 0 / data 为数组：把指定目录作为唯一的根节点
    if (node.level === 0 || Array.isArray(node.data)) {
        resolve([
            {
                path: props.rootPath,
                name: basename(props.rootPath) || props.rootPath,
                isDir: true,
                isLeaf: false,
            },
        ])
        return
    }
    // 防御：文件节点不应展开（正常情况 isLeaf 已阻止出现展开箭头）
    if (!node.data.isDir) {
        resolve([])
        return
    }
    try {
        const list = await props.backend.list(node.data.path)
        const children = (list ?? [])
            .map((e) => ({
                path: e.path,
                name: e.name,
                isDir: e.isDir,
                // 关键：懒加载树用 isLeaf 判断叶子节点（且 el-tree 的 props 必须显式
                // 配置 isLeaf: 'isLeaf'，否则默认不读该字段，文件仍会被当成目录）
                isLeaf: !e.isDir,
            }))
            // 目录归目录放前面、文件归文件放后面，各自按名称排序（自然排序：file2 < file10）
            .sort((a, b) => {
                if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
                return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
            })
        resolve(children)
    } catch (e: any) {
        ElMessage.error(`加载目录失败：${e?.message || e}`)
        resolve([])
    }
}

// 挂载 / 刷新后自动展开根目录，立即显示指定目录的内容
async function expandRoot() {
    await nextTick()
    try {
        const tree = treeRef.value as any
        const root = tree?.store?.nodesMap?.[props.rootPath]
        if (root && !root.expanded) root.expand()
    } catch {
        /* 树未就绪时忽略 */
    }
}

function refreshRoot() {
    treeVersion.value++
    selectedNode.value = null // 整树重建，清空已失效的选中引用
    void expandRoot()
}

onMounted(expandRoot)

function onNodeClick(data: any) {
    selectedNode.value = { path: data.path, name: data.name, isDir: data.isDir }
    if (!data.isDir) {
        void openFile({ path: data.path, name: data.name })
    }
}

// ---------- 标签切换自动定位（设置 editor.treeFollow 控制） ----------
// 切换已打开文件的标签时，左侧文件树自动展开所在目录、选中并滚动到该文件。

// 等待某个路径的树节点出现（懒加载目录展开后子节点异步填充）
function waitForNode(path: string, timeoutMs = 4000): Promise<any> {
    const tree = treeRef.value as any
    const t0 = Date.now()
    return new Promise((resolve) => {
        const tick = () => {
            const n = tree?.store?.nodesMap?.[path]
            if (n) return resolve(n)
            if (Date.now() - t0 > timeoutMs) return resolve(null)
            setTimeout(tick, 60)
        }
        tick()
    })
}

// 在懒加载树中定位并选中某个文件：逐级展开祖先目录，最后高亮 + 滚动到可见区域。
// 目录未加载时强制展开触发 loadNode，等子节点出现后再继续下钻（尽力而为，失败静默）。
async function revealInTree(path: string) {
    const tree = treeRef.value as any
    if (!tree) return
    const root = tree.store?.nodesMap?.[props.rootPath]
    if (root && !root.expanded) root.expand()
    let rel = path
    if (path === props.rootPath) {
        return
    }
    if (path.startsWith(props.rootPath + '/') || path.startsWith(props.rootPath + '\\')) {
        rel = path.slice(props.rootPath.length)
    }
    rel = rel.replace(/^[\\/]+/, '')
    if (!rel) return
    const sep = path.includes('/') ? '/' : path.includes('\\') ? '\\' : props.backend.sep
    const segs = rel.split(/[\\/]/).filter(Boolean)
    let cur = props.rootPath
    for (let i = 0; i < segs.length; i++) {
        const childPath = joinPath(cur, segs[i], sep)
        let node = tree.store?.nodesMap?.[childPath]
        if (!node) {
            // 父目录尚未加载/展开：调用 expand()（未加载时会触发懒加载，子节点异步填充）
            const parent = tree.store?.nodesMap?.[cur]
            if (parent && !parent.expanded) parent.expand()
            node = await waitForNode(childPath)
            if (!node) return // 超时或加载失败，放弃定位（不打扰用户）
        }
        if (i === segs.length - 1) {
            tree.setCurrentKey(childPath)
            selectedNode.value = {
                path: childPath,
                name: node.data?.name ?? segs[i],
                isDir: node.data?.isDir ?? false,
            }
            const current = tree.$el?.querySelector?.('.el-tree-node.is-current')
            current?.scrollIntoView?.({ block: 'nearest' })
            return
        }
        if (node.data?.isDir && !node.expanded) node.expand()
        cur = childPath
    }
}

watch(activeKey, (key) => {
    if (!key || !settings.editorTreeFollow) return
    const f = openFiles.value.find((x) => x.key === key)
    if (!f) return
    void revealInTree(f.path)
})

// 重新加载某个目录节点的子级（懒加载树）。节点不在当前已加载集合中时，
// 回退为整体刷新（重新挂树并展开根目录）。
function reloadDir(path: string) {
    const tree = treeRef.value as any
    const node = tree?.getNode?.(path)
    if (!node) {
        refreshRoot()
        return
    }
    node.loaded = false
    if (node.expanded) node.expand()
}

// 刷新「当前上下文」：选中目录 → 只刷新该目录；选中文件 → 刷新其父目录；未选中 → 整棵树刷新
function refreshCurrent() {
    const n = selectedNode.value
    if (n) {
        reloadDir(n.isDir ? n.path : parentDir(n.path, props.backend.sep))
    } else {
        refreshRoot()
    }
}

// ---------- 内嵌搜索面板（VS Code 式切换，替代弹出式对话框） ----------

// 切换目录树 ↔ 搜索面板；搜索范围取当前选中目录（未选中时用根目录）
async function toggleSearch() {
    if (showSearch.value) {
        showSearch.value = false
        return
    }
    showSearch.value = true
    await nextTick()
    searchRef.value?.open(currentDir.value)
}

function onSearchPick(r: SearchResult) {
    if (r.isDir) {
        showSearch.value = false
        void revealPath(r.path)
    } else {
        void openFile({ path: r.path, name: r.name }, r.lineNo)
    }
}

// 展开懒加载树直至目标目录（搜索结果双击目录后定位到树中对应节点）
async function revealPath(path: string) {
    if (path === props.rootPath) {
        refreshRoot()
        return
    }
    const chain: string[] = []
    let cur = path
    while (cur && cur !== props.rootPath) {
        chain.unshift(cur)
        const p = parentDir(cur, props.backend.sep)
        if (p === cur) break
        cur = p
    }
    const tree = treeRef.value as any
    const store = tree?.store
    if (!store) return
    const expandAt = (i: number) => {
        if (i >= chain.length) {
            treeRef.value?.setCurrentKey?.(path)
            return
        }
        const node = store.nodesMap[chain[i]]
        if (node) {
            node.expand(() => expandAt(i + 1))
        }
    }
    expandAt(0)
}

// ---------- 右键上传 / 下载（仅远程后端） ----------

async function pickUploadFiles() {
    const dir = currentDir.value
    let files: string[]
    try {
        files = (await LocalService.PickFiles()) ?? []
    } catch (e: any) {
        ElMessage.error(`选择文件失败：${e?.message || e}`)
        return
    }
    if (!files.length) return
    let ok = 0
    const failed: string[] = []
    for (const f of files) {
        const target = joinPath(dir, basename(f), props.backend.sep)
        try {
            await props.backend.upload(f, target)
            ok++
        } catch (e: any) {
            failed.push(basename(f))
            ElMessage.error(`上传 ${basename(f)} 失败：${e?.message || e}`)
        }
    }
    if (failed.length === 0) ElMessage.success(`已上传 ${ok} 项到 ${dir}`)
    await reloadDir(dir)
}

async function pickUploadDir() {
    const dir = currentDir.value
    let local: string
    try {
        local = (await LocalService.PickDirectory()) || ''
    } catch (e: any) {
        ElMessage.error(`选择目录失败：${e?.message || e}`)
        return
    }
    if (!local) return
    const name = basename(local)
    const target = joinPath(dir, name, props.backend.sep)
    try {
        await props.backend.upload(local, target)
        ElMessage.success(`已上传目录到 ${dir}`)
    } catch (e: any) {
        ElMessage.error(`上传目录失败：${e?.message || e}`)
    }
    await reloadDir(dir)
}

// 选择下载保存目录；取消时回退到系统下载目录
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

async function downloadSelected() {
    const n = selectedNode.value
    if (!n || n.path === props.rootPath) {
        ElMessage.warning('请先选择要下载的文件或目录')
        return
    }
    const dir = await pickDownloadDir()
    if (!dir) return
    const target = joinPath(dir, n.name, '/')
    try {
        await props.backend.download(n.path, target, n.isDir)
        ElMessage.success(`已下载到 ${target}`)
    } catch (e: any) {
        ElMessage.error(`下载失败：${e?.message || e}`)
    }
}

// 检查目标目录下是否已存在同名条目，避免新建文件时静默覆盖已有文件
async function nameExists(dir: string, name: string): Promise<boolean> {
    try {
        const list = await props.backend.list(dir)
        return (list ?? []).some((e) => e.name === name)
    } catch {
        return false // 列目录失败时不拦截，交给后端报错
    }
}

async function createFile() {
    const values = await showInputDialog('新建文件', [{ key: 'name', label: '文件名称' }])
    if (!values) return
    const name = values.name.trim()
    if (!name) return
    const dir = currentDir.value
    if (await nameExists(dir, name)) {
        ElMessage.warning(`「${name}」已存在`)
        return
    }
    const target = joinPath(dir, name, props.backend.sep)
    try {
        await props.backend.writeFile(target, '')
        ElMessage.success('已创建')
        await reloadDir(dir)
    } catch (e: any) {
        ElMessage.error(`创建文件失败：${e?.message || e}`)
    }
}

async function mkdir() {
    const values = await showInputDialog('新建目录', [{ key: 'name', label: '目录名称' }])
    if (!values) return
    const name = values.name.trim()
    if (!name) return
    const dir = currentDir.value
    if (await nameExists(dir, name)) {
        ElMessage.warning(`「${name}」已存在`)
        return
    }
    const target = joinPath(dir, name, props.backend.sep)
    try {
        await props.backend.mkdir(target)
        ElMessage.success('已创建')
        await reloadDir(dir)
    } catch (e: any) {
        ElMessage.error(`创建目录失败：${e?.message || e}`)
    }
}

async function renameNode() {
    const n = selectedNode.value
    if (!n || n.path === props.rootPath) {
        ElMessage.warning('请先选择要重命名的文件或目录')
        return
    }
    const values = await showInputDialog('重命名', [{ key: 'name', label: '新名称', initial: n.name }])
    if (!values) return
    const name = values.name.trim()
    if (!name || name === n.name) return
    const dir = parentDir(n.path, props.backend.sep)
    const target = joinPath(dir, name, props.backend.sep)
    try {
        await props.backend.rename(n.path, target)
        // 若被重命名的文件正打开着，同步更新标签与编辑器实例的 key
        const open = openFiles.value.find((f) => f.key === n.path)
        if (open) {
            editorRefs.value[target] = editorRefs.value[n.path]
            delete editorRefs.value[n.path]
            open.key = target
            open.path = target
            open.name = name
            if (activeKey.value === n.path) activeKey.value = target
        }
        selectedNode.value = { path: target, name, isDir: n.isDir }
        ElMessage.success('已重命名')
        await reloadDir(dir)
    } catch (e: any) {
        ElMessage.error(`重命名失败：${e?.message || e}`)
    }
}

async function removeNode() {
    const n = selectedNode.value
    if (!n || n.path === props.rootPath) {
        ElMessage.warning('请先选择要删除的文件或目录')
        return
    }
    const tip = n.isDir
        ? `确定删除目录「${n.name}」及其全部内容？`
        : `确定删除文件「${n.name}」？`
    const ok = await showConfirmDialog('删除', tip, true, '删除')
    if (!ok) return
    try {
        await props.backend.remove(n.path, n.isDir)
        // 关闭已删除文件的标签（不询问未保存）
        const idx = openFiles.value.findIndex((f) => f.key === n.path)
        if (idx >= 0) {
            const f = openFiles.value[idx]
            openFiles.value.splice(idx, 1)
            delete editorRefs.value[f.key]
            if (activeKey.value === f.key) {
                activeKey.value = openFiles.value[idx]?.key ?? openFiles.value[idx - 1]?.key ?? null
            }
        }
        selectedNode.value = null
        ElMessage.success('已删除')
        await reloadDir(parentDir(n.path, props.backend.sep))
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

async function chmodNode() {
    const n = selectedNode.value
    if (!n || n.path === props.rootPath) {
        ElMessage.warning('请先选择要修改权限的文件或目录')
        return
    }
    if (!props.backend.chmod) return
    const values = await showInputDialog('修改权限', [
        { key: 'mode', label: '八进制权限（如 755）', initial: '755' },
    ])
    if (!values) return
    const mode = parseInt(values.mode.trim(), 8)
    if (isNaN(mode)) {
        ElMessage.error('权限格式不正确')
        return
    }
    try {
        await props.backend.chmod!(n.path, mode)
        ElMessage.success('已修改')
        await reloadDir(parentDir(n.path, props.backend.sep))
    } catch (e: any) {
        ElMessage.error(`修改权限失败：${e?.message || e}`)
    }
}

// ---------- 目录树拖拽移动（文件 / 目录拖到其他文件夹） ----------

// 仅允许「拖入文件夹内部」（inner）；不允许插到同级节点前后，也不允许把目录拖进自己的子孙目录。
function allowDrop(dragging: any, drop: any, type: string): boolean {
    if (type !== 'inner') return false
    const src = dragging?.data as TreeNode | undefined
    const dst = drop?.data as TreeNode | undefined
    if (!src || !dst || !dst.isDir) return false
    if (src.path === dst.path) return false
    if (src.isDir) {
        const sep = src.path.includes('/') ? '/' : '\\'
        if (dst.path.startsWith(src.path + sep)) return false // 拖进自己的子目录 = 循环移动
    }
    return true
}

// 拖拽完成：把条目 rename 到目标目录（本地 / SFTP / FTP 的 rename 均为路径级移动）。
// 同步更新已打开标签的路径（被移动的文件本身，或位于被移动目录内的文件），并刷新源 / 目标目录。
async function onNodeDrop(dragging: any, drop: any, dropType: string) {
    if (dropType !== 'inner') return
    const src = dragging.data as TreeNode
    const dst = drop.data as TreeNode
    if (!src || !dst || src.path === dst.path) return
    const target = joinPath(dst.path, src.name, props.backend.sep)
    if (target === src.path) return
    if (await nameExists(dst.path, src.name)) {
        ElMessage.warning(`目标目录已存在同名「${src.name}」`)
        return
    }
    const srcDir = parentDir(src.path, props.backend.sep)
    try {
        await props.backend.rename(src.path, target)
        updateOpenPaths(src.path, target, src.isDir)
        if (selectedNode.value?.path === src.path) {
            selectedNode.value = { path: target, name: src.name, isDir: src.isDir }
        }
        ElMessage.success(`已移动到 ${dst.name}`)
        await reloadDir(srcDir)
        await reloadDir(dst.path)
    } catch (e: any) {
        ElMessage.error(`移动失败：${e?.message || e}`)
        await reloadDir(srcDir)
        await reloadDir(dst.path)
    }
}

// 移动后同步打开的标签：文件本身被移动 → 直接改 key/path；目录被移动 → 目录内已打开的文件路径一并前移
function updateOpenPaths(oldPath: string, newPath: string, isDir: boolean) {
    for (const f of openFiles.value) {
        let np: string | null = null
        if (!isDir && f.key === oldPath) {
            np = newPath
        } else if (
            isDir &&
            (f.key === oldPath || f.key.startsWith(oldPath + '/') || f.key.startsWith(oldPath + '\\'))
        ) {
            np = newPath + f.key.slice(oldPath.length)
        }
        if (!np) continue
        const oldKey = f.key
        editorRefs.value[np] = editorRefs.value[oldKey]
        delete editorRefs.value[oldKey]
        f.key = np
        f.path = np
        f.name = basename(np)
        if (activeKey.value === oldKey) activeKey.value = np
    }
}

// ---------- 目录树右键菜单 ----------

function openCtx(e: MouseEvent) {
    ctxX.value = e.clientX
    ctxY.value = e.clientY
    ctxVisible.value = false
    requestAnimationFrame(() => {
        ctxVisible.value = true
    })
}

function buildMenu(data: TreeNode | null): (CtxItem | 'divider')[] {
    const isRoot = !data || (data.isDir && data.path === props.rootPath)
    const items: (CtxItem | 'divider')[] = [
        { key: 'create-file', label: '新建文件', icon: DocumentAdd },
        { key: 'mkdir', label: '新建目录', icon: FolderAdd },
        'divider',
    ]
    if (isRemote.value) {
        items.push({ key: 'upload-files', label: '上传文件…', icon: Upload })
        items.push({ key: 'upload-dir', label: '上传目录…', icon: FolderAdd })
        if (!isRoot) {
            items.push({
                key: 'download-entry',
                label: data.isDir ? '下载目录（含全部内容）' : '下载',
                icon: Download,
            })
        }
        items.push('divider')
    }
    if (!isRoot) {
        if (data.isDir) {
            items.push({ key: 'refresh', label: '刷新', icon: Refresh })
        }
        items.push({ key: 'rename', label: '重命名', icon: Edit })
        if (props.backend.chmod) {
            items.push({ key: 'chmod', label: '修改权限', icon: Lock })
        }
        items.push({ key: 'remove', label: '删除', icon: Delete, danger: true })
    } else {
        items.push({ key: 'refresh', label: '刷新', icon: Refresh })
    }
    return items
}

function onNodeContextMenu(event: MouseEvent, data: any) {
    event.preventDefault()
    event.stopPropagation()
    selectedNode.value = { path: data.path, name: data.name, isDir: data.isDir }
    ctxItems.value = buildMenu(selectedNode.value)
    openCtx(event)
}

function onBlankContextMenu(event: MouseEvent) {
    event.preventDefault()
    selectedNode.value = null
    ctxItems.value = buildMenu(null)
    openCtx(event)
}

async function onCtxPick(item: CtxItem) {
    switch (item.key) {
        case 'create-file':
            await createFile()
            break
        case 'mkdir':
            await mkdir()
            break
        case 'rename':
            await renameNode()
            break
        case 'chmod':
            await chmodNode()
            break
        case 'remove':
            await removeNode()
            break
        case 'refresh':
            refreshCurrent()
            break
        case 'upload-files':
            await pickUploadFiles()
            break
        case 'upload-dir':
            await pickUploadDir()
            break
        case 'download-entry':
            await downloadSelected()
            break
    }
}

// ---------- 文件标签 ----------

// 正在读取中的路径集合：防止同一文件被多次同时打开（如树点击与跳转后的
// 自动打开并发），对怪癖 FTP 服务器会避免重复 RETR 造成的偶发 550。
const loadingPaths = new Set<string>()

async function openFile(file: { path: string; name: string }, lineNo?: number) {
    const kind: 'text' | 'md' = file.name.toLowerCase().endsWith('.md') ? 'md' : 'text'
    const key = file.path
    const existing = openFiles.value.find((f) => f.key === key)
    if (existing) {
        activeKey.value = key
        return
    }
    if (loadingPaths.has(key)) return // 已在读取中，跳过重复请求
    loadingPaths.add(key)
    loadingFile.value = true
    try {
        const content = await props.backend.readFile(file.path)
        if (!openFiles.value.some((f) => f.key === key)) {
            const f: OpenFile = { key, path: file.path, name: file.name, kind, original: content, dirty: false }
            openFiles.value.push(f)
        }
        activeKey.value = key
        await nextTick()
        editorRefs.value[key]?.setContent(content)
        if (lineNo) {
            ;(editorRefs.value[key] as any)?.jumpToLine?.(lineNo)
        } else {
            editorRefs.value[key]?.focus()
        }
    } catch (e: any) {
        ElMessage.error(`打开文件失败：${e?.message || e}`)
    } finally {
        loadingPaths.delete(key)
        loadingFile.value = false
    }
}

// 供父组件调用：按路径打开一个文件（如在文件列表中双击远程文件）
async function openPath(path: string) {
    await openFile({ path, name: basename(path) })
}

function onFileChange(f: OpenFile, value: string) {
    f.dirty = value !== f.original
}

async function saveActive(): Promise<boolean> {
    const f = activeFile.value
    if (!f) return true
    const ed = editorRefs.value[f.key]
    if (!ed) return true
    const content = ed.getContent()
    saving.value = true
    try {
        await props.backend.writeFile(f.path, content)
        f.original = content
        f.dirty = false
        ElMessage.success('已保存')
        return true
    } catch (e: any) {
        ElMessage.error(`保存失败：${e?.message || e}`)
        return false
    } finally {
        saving.value = false
    }
}

async function closeFile(f: OpenFile) {
    if (f.dirty) {
        const ok = await showConfirmDialog('关闭文件', `「${f.name}」有未保存的更改，确定关闭？`, true, '不保存并关闭')
        if (!ok) return
    }
    const idx = openFiles.value.findIndex((x) => x.key === f.key)
    if (idx < 0) return
    openFiles.value.splice(idx, 1)
    delete editorRefs.value[f.key] // 释放编辑器实例（组件卸载时销毁）
    if (activeKey.value === f.key) {
        activeKey.value = openFiles.value[idx]?.key ?? openFiles.value[idx - 1]?.key ?? null
    }
}

// ---------- 文件标签页右键 ----------

function onTabContext(event: MouseEvent, f: OpenFile) {
    event.preventDefault()
    tabCtxFile.value = f
    tabCtxIndex.value = openFiles.value.findIndex((x) => x.key === f.key)
    tabCtxItems.value = buildTabCtx()
    tabCtxX.value = event.clientX
    tabCtxY.value = event.clientY
    tabCtxVisible.value = false
    requestAnimationFrame(() => {
        tabCtxVisible.value = true
    })
}

function buildTabCtx(): (CtxItem | 'divider')[] {
    const total = openFiles.value.length
    const idx = tabCtxIndex.value
    return [
        { key: 'close-current', label: '关闭当前', icon: Close, disabled: total === 0 },
        { key: 'close-others', label: '关闭其他', icon: CloseBold, disabled: total <= 1 },
        { key: 'close-right', label: '关闭右边', icon: DArrowRight, disabled: idx < 0 || idx >= total - 1 },
        { key: 'close-all', label: '关闭全部', icon: CircleClose, disabled: total === 0 },
    ]
}

async function onTabCtxPick(item: CtxItem) {
    const f = tabCtxFile.value
    if (!f) return
    const idx = openFiles.value.findIndex((x) => x.key === f.key)
    switch (item.key) {
        case 'close-current':
            await closeFile(f)
            break
        case 'close-others':
            await closeFiles(openFiles.value.filter((x) => x.key !== f.key))
            break
        case 'close-right':
            await closeFiles(idx >= 0 ? openFiles.value.slice(idx + 1) : [])
            break
        case 'close-all':
            await closeFiles([...openFiles.value])
            break
    }
}

async function closeFiles(list: OpenFile[]) {
    if (!list.length) return
    const dirtyCount = list.filter((f) => f.dirty).length
    if (dirtyCount > 0) {
        const ok = await showConfirmDialog(
            '关闭文件',
            `有 ${dirtyCount} 个文件存在未保存的更改，确定关闭？`,
            true,
            '不保存并关闭',
        )
        if (!ok) return
    }
    for (const f of list) {
        const idx = openFiles.value.findIndex((x) => x.key === f.key)
        if (idx < 0) continue
        openFiles.value.splice(idx, 1)
        delete editorRefs.value[f.key]
        if (activeKey.value === f.key) {
            activeKey.value = openFiles.value[idx]?.key ?? openFiles.value[idx - 1]?.key ?? null
        }
    }
}

// 板块关闭前确认：有未保存文件时询问
async function confirmClose(): Promise<boolean> {
    const dirtyOnes = openFiles.value.filter((f) => f.dirty)
    if (dirtyOnes.length === 0) return true
    const names = dirtyOnes.slice(0, 3).map((f) => f.name).join('、')
    const ok = await showConfirmDialog(
        '关闭编辑器板块',
        `仍有 ${dirtyOnes.length} 个文件未保存（${names}${dirtyOnes.length > 3 ? '…' : ''}），确定关闭板块？`,
        true,
        '不保存并关闭',
    )
    return ok
}

defineExpose({ openPath, confirmClose, hasDirty: () => openFiles.value.some((f) => f.dirty) })
</script>

<style scoped>
.remote-editor-panel {
    height: 100%;
    min-height: 0;
}

.remote-editor-panel :deep(.el-splitter) {
    height: 100%;
}

.remote-editor-panel :deep(.el-splitter-panel) {
    min-width: 0;
}

.remote-editor-panel :deep(.el-splitter-panel:not(:last-child)) {
    padding-right: 8px;
}

.rep-tree {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
}

.rep-tree-head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 2px;
    padding: 4px 6px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.rep-root {
    flex: 1;
    min-width: 0;
    font-family: var(--term-font);
    font-size: 12px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.rep-tree-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 4px;
}

.rep-tree-body.rep-search-body {
    padding: 0;
    overflow: hidden;
}

.rep-tree-head :deep(.el-button.active) {
    color: var(--active-text);
    background: var(--hover-strong);
}

.rep-tree-node {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    overflow: hidden;
}

.rep-tree-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.rep-editor {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
}

.rep-tabs {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 4px 6px 0;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
    overflow-x: auto;
}

.rep-tab {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 5px 9px;
    font-size: 12.5px;
    color: var(--text-secondary);
    /* background: var(--panel-soft);
    border: 1px solid var(--border-color); */
    border-bottom: none;
    border-radius: 6px 6px 0 0;
    cursor: pointer;
    max-width: 200px;
    flex-shrink: 0;
    user-select: none;
    margin-bottom: -3px;
}

.rep-tab.active {
    background: var(--active-bg);
    color: var(--active-text);
}

.rep-tab-title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.rep-dirty {
    font-size: 10px;
    color: #e6c06c;
    flex-shrink: 0;
}

.rep-tab-close {
    color: var(--text-secondary);
    border-radius: 3px;
    flex-shrink: 0;
}

.rep-tab-close:hover {
    color: #f56c6c;
    background: rgba(245, 108, 108, 0.15);
}

.rep-tab-actions {
    margin-left: auto;
    padding: 0 2px 4px;
    flex-shrink: 0;
}

.rep-editor-area {
    flex: 1;
    min-height: 0;
    position: relative;
}

.rep-pane {
    position: absolute;
    inset: 0;
}

.rep-empty {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--text-secondary);
    background: var(--editor-bg);
}

.rep-empty p {
    margin: 0;
    font-size: 13px;
}

.rep-empty .sub {
    font-size: 12px;
    opacity: 0.7;
}

.rep-loading {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    background: var(--overlay-bg);
    z-index: 5;
    color: var(--text-secondary);
    font-size: 13px;
}
</style>
