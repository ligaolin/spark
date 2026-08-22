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
                        <el-button size="small" text title="重命名" :disabled="!canOperateSelected" @click="renameNode">
                            <el-icon><Edit /></el-icon>
                        </el-button>
                        <el-button size="small" text title="删除" :disabled="!canOperateSelected" @click="removeNode">
                            <el-icon><Delete /></el-icon>
                        </el-button>
                        <el-button v-if="canChmod" size="small" text title="修改权限" :disabled="!canOperateSelected" @click="chmodNode">
                            <el-icon><Lock /></el-icon>
                        </el-button>
                        <el-button size="small" text title="刷新当前目录" @click="refreshCurrent">
                            <el-icon><Refresh /></el-icon>
                        </el-button>
                    </div>
                    <div class="rep-tree-body" @contextmenu.prevent="onBlankContextMenu">
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
                            @node-click="onNodeClick"
                            @node-contextmenu="onNodeContextMenu"
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
                            :class="{ active: f.key === activeKey }" @click="activeKey = f.key">
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
    </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
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
} from '@element-plus/icons-vue'
import CodeEditor from './CodeEditor.vue'
import MarkdownEditor from './MarkdownEditor.vue'
import ContextMenu from './ContextMenu.vue'
import type { CtxItem } from './ContextMenu.vue'
import { showConfirmDialog, showInputDialog } from '../utils/dialog'
import { useSettingsStore } from '../stores/settings'
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

// FTP 后端没有 chmod 能力（仅 SFTP 提供）
const canChmod = computed(() => !!props.backend.chmod)
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
        const children = (list ?? []).map((e) => ({
            path: e.path,
            name: e.name,
            isDir: e.isDir,
            // 关键：懒加载树用 isLeaf 判断叶子节点（且 el-tree 的 props 必须显式
            // 配置 isLeaf: 'isLeaf'，否则默认不读该字段，文件仍会被当成目录）
            isLeaf: !e.isDir,
        }))
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
    background: var(--panel-soft);
    border: 1px solid var(--border-color);
    border-bottom: none;
    border-radius: 6px 6px 0 0;
    cursor: pointer;
    max-width: 200px;
    flex-shrink: 0;
    user-select: none;
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
