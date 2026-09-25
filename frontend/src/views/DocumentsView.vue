<template>
    <div class="doc-view">
        <!-- 顶部工具栏 -->
        <div class="doc-toolbar">
            <el-button size="small" type="primary" plain @click="toolbarCreate('file')">
                <el-icon>
                    <DocumentAdd />
                </el-icon><span>新建文件</span>
            </el-button>
            <el-button size="small" plain @click="toolbarCreate('folder')">
                <el-icon>
                    <FolderAdd />
                </el-icon><span>新建文件夹</span>
            </el-button>
            <el-button size="small" :disabled="!selectedId" @click="renameSelected">重命名</el-button>
            <el-button size="small" type="danger" plain :disabled="!selectedId" @click="deleteSelected">删除</el-button>

            <el-dropdown trigger="click" :disabled="!activeTab" @command="onAiCommand">
                <el-button size="small" :disabled="!activeTab">
                    <el-icon><MagicStick /></el-icon><span>AI</span>
                </el-button>
                <template #dropdown>
                    <el-dropdown-menu>
                        <el-dropdown-item command="summarize">总结全文</el-dropdown-item>
                        <el-dropdown-item command="rewrite">改写选中 / 全文</el-dropdown-item>
                        <el-dropdown-item command="translate">翻译选中 / 全文</el-dropdown-item>
                        <el-dropdown-item command="generate">生成内容…</el-dropdown-item>
                    </el-dropdown-menu>
                </template>
            </el-dropdown>

            <div class="toolbar-spacer" />

            <el-input v-model="keyword" size="small" placeholder="搜索文档（文件名 / 内容）" clearable style="width: 230px"
                @keyup.enter="runSearch" />
            <el-radio-group v-model="searchMode" size="small">
                <el-radio-button value="name">文件名</el-radio-button>
                <el-radio-button value="content">内容</el-radio-button>
            </el-radio-group>
            <el-button size="small" type="primary" :loading="searching" @click="runSearch">搜索</el-button>
        </div>

        <!-- 主体：左树 + 右多标签编辑器 -->
        <div class="doc-body">
            <el-splitter>
                <el-splitter-panel size="26%" :min="200">
                    <div class="doc-left">
                        <div class="left-tabs">
                            <div class="left-tab" :class="{ active: leftTab === 'tree' }" @click="leftTab = 'tree'">文件</div>
                            <div class="left-tab" :class="{ active: leftTab === 'search' }" @click="leftTab = 'search'">
                                搜索<span v-if="results.length" class="tab-badge">{{ results.length }}</span>
                            </div>
                            <div class="left-tab-action">
                                <el-button size="small" text @click="reload">
                                    <el-icon :class="{ spinning: reloading }"><Refresh /></el-icon>
                                </el-button>
                            </div>
                        </div>

                        <div v-show="leftTab === 'tree'" class="tree-wrap" @contextmenu="onBlankContext">
                            <el-tree ref="treeRef" :data="treeData" node-key="id"
                                :props="{ label: 'name', children: 'children', isLeaf: 'leaf' }" highlight-current
                                :expand-on-click-node="true" draggable lazy :load="loadNode"
                                :allow-drop="allowDrop" @node-click="onNodeClick"
                                @node-contextmenu="onNodeContext" @node-drop="onNodeDrop"
                                empty-text="暂无文档，点击上方「新建文件」开始">
                                <template #default="{ data }">
                                    <span class="tree-node">
                                        <el-icon :color="data.type === 'folder' ? '#e6c06c' : data.kind === 'md' ? '#67c23a' : 'var(--text-secondary)'">
                                            <Folder v-if="data.type === 'folder'" />
                                            <Document v-else />
                                        </el-icon>
                                        <span class="tree-node-name" :title="data.name">{{ data.name }}</span>
                                        <el-tag v-if="data.type === 'file' && data.kind === 'md'" size="small"
                                            type="success" class="md-tag">MD</el-tag>
                                    </span>
                                </template>
                            </el-tree>
                        </div>

                        <div v-show="leftTab === 'search'" class="results-wrap">
                            <el-table :data="results" size="small" height="100%" empty-text="输入关键字后点击「搜索」"
                                @row-dblclick="openResult">
                                <el-table-column label="名称" min-width="130">
                                    <template #default="{ row }">
                                        <el-icon v-if="row.isDir" color="#e6c06c">
                                            <Folder />
                                        </el-icon>
                                        <el-icon v-else color="var(--text-secondary)">
                                            <Document />
                                        </el-icon>
                                        <span class="res-name" :title="row.name">{{ row.name }}</span>
                                    </template>
                                </el-table-column>
                                <el-table-column label="路径" prop="path" min-width="160" show-overflow-tooltip />
                                <el-table-column v-if="searchMode === 'content'" label="行" width="48" align="right">
                                    <template #default="{ row }"><span class="dim">{{ row.lineNo }}</span></template>
                                </el-table-column>
                            </el-table>
                        </div>
                    </div>
                </el-splitter-panel>

                <el-splitter-panel :min="200">
                    <div class="doc-right">
                        <!-- 多标签：同时打开多个文档 -->
                        <div class="doc-tabbar">
                            <div v-for="t in tabs" :key="t.key" class="doc-tab"
                                :class="{ active: t.key === activeKey }" @click="activeKey = t.key"
                                @contextmenu.prevent="onTabContext($event, t)">
                                <span class="doc-tab-title" :title="t.path">{{ t.name }}</span>
                                <span v-if="t.dirty" class="doc-tab-dirty" title="未保存">●</span>
                                <el-icon class="doc-tab-close" title="关闭" @click.stop="closeTab(t)">
                                    <Close />
                                </el-icon>
                            </div>
                            <div class="doc-tab-actions">
                                <el-button size="small" type="primary" :disabled="!activeTab" :loading="saving"
                                    @click="save">
                                    保存
                                </el-button>
                            </div>
                        </div>

                        <div class="editor-area">
                            <KeepAlive :max="6">
                                <MarkdownEditor v-if="activeTab?.kind === 'md'" :key="'md-' + activeKey!"
                                    class="doc-pane"
                                    :ref="(el: any) => setEditorRef(activeKey!, el)"
                                    @change="(v: string) => onChange(activeTab!, v)" @save="save" />
                                <CodeEditor v-else-if="activeTab" :key="'code-' + activeKey!"
                                    class="doc-pane"
                                    :filename="activeTab.name"
                                    :wrap="settings.editorWordWrap"
                                    :ref="(el: any) => setEditorRef(activeKey!, el)"
                                    @change="(v: string) => onChange(activeTab!, v)" @save="save" />
                            </KeepAlive>
                            <div v-if="!tabs.length" class="editor-empty">
                                <el-icon :size="40">
                                    <Document />
                                </el-icon>
                                <p>在左侧选择或新建一个文档开始编辑</p>
                                <p class="sub">支持多标签同时编辑、语法高亮、搜索（Ctrl+F）、Ctrl+S 保存；新建 Markdown 文档可排版预览</p>
                            </div>
                        </div>
                    </div>
                </el-splitter-panel>
            </el-splitter>
        </div>

        <ContextMenu v-model="ctxVisible" :x="ctxX" :y="ctxY" :items="ctxItems" @pick="onCtxPick" />
        <ContextMenu v-model="tabCtxVisible" :x="tabCtxX" :y="tabCtxY" :items="tabCtxItems" @pick="onTabCtxPick" />
    </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
    Folder,
    Document,
    FolderAdd,
    DocumentAdd,
    Edit,
    Delete,
    Close,
    CloseBold,
    DArrowRight,
    CircleClose,
    MagicStick,
    Refresh,
} from '@element-plus/icons-vue'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import CodeEditor from '../components/CodeEditor.vue'
import MarkdownEditor from '../components/MarkdownEditor.vue'
import { DocumentService } from '../utils/wails'
import type { DocNode } from '../utils/wails'
import { showInputDialog, showConfirmDialog } from '../utils/dialog'
import { streamToDialog } from '../utils/ai'
import { kindForName } from '../utils/fileKind'
import { useSettingsStore } from '../stores/settings'
import type { SearchResult } from '../types'

interface TreeNode {
    id: number
    parentId: number
    name: string
    type: string
    kind: string
    sort: number
    path: string
    leaf?: boolean
    children?: TreeNode[]
}

// 两个编辑器组件暴露同一套操作方法，统一为一个接口
interface EditorApi {
    setContent(v: string): void
    getContent(): string
    focus(): void
    jumpToLine(n?: number): void
    // 代码编辑器额外支持（Markdown 编辑器无这些方法）
    getSelection?(): string
    replaceSelection?(text: string): void
    insertAtCursor?(text: string): void
}

// 一个打开的文档标签
interface DocTab {
    key: string
    id: number
    name: string
    path: string
    // 文档类型（'text' | 'md'，未来可扩展）：决定用哪个编辑器渲染
    kind: string
    original: string
    dirty: boolean
}

const treeRef = ref()
const reloading = ref(false)
const settings = useSettingsStore()

const tabs = ref<DocTab[]>([])
const activeKey = ref<string | null>(null)
const editorRefs = ref<Record<string, EditorApi | null>>({})
const saving = ref(false)

const activeTab = computed(() => tabs.value.find((t) => t.key === activeKey.value) ?? null)

// 无论通过点击标签还是树节点切换，KeepAlive 驱逐后重建的编辑器需要恢复内容
watch(activeKey, async (key) => {
    if (!key) return
    const tab = tabs.value.find((t) => t.key === key)
    if (!tab) return
    await nextTick()
    const ed = editorRefs.value[key]
    if (ed && !ed.getContent()) {
        ed.setContent(tab.original)
    }
})

const selectedId = ref<number | null>(null)

// 搜索
const keyword = ref('')
const searchMode = ref<'name' | 'content'>('name')
const searching = ref(false)
const results = ref<SearchResult[]>([])
const leftTab = ref<'tree' | 'search'>('tree')

// 右键菜单
const ctxVisible = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)
const ctxItems = ref<(CtxItem | 'divider')[]>([])
const ctxNode = ref<TreeNode | null>(null)

// 标签页右键菜单
const tabCtxVisible = ref(false)
const tabCtxX = ref(0)
const tabCtxY = ref(0)
const tabCtxItems = ref<(CtxItem | 'divider')[]>([])
const tabCtxTab = ref<DocTab | null>(null)
const tabCtxIndex = ref(-1)

// ---------- 懒加载节点缓存 ----------

const nodeCache = reactive<Record<number, TreeNode>>({})

function toTreeNode(n: DocNode, parentPath: string): TreeNode {
    const path = parentPath ? parentPath + '/' + n.name : '/' + n.name
    return {
        id: n.id,
        parentId: n.parentId,
        name: n.name,
        type: n.type,
        kind: n.kind || kindForName(n.name),
        sort: n.sort || 0,
        path,
        leaf: n.type === 'file',
    }
}

function sortTreeNodes(arr: TreeNode[]) {
    arr.sort((a, b) => {
        if (a.type !== b.type) return a.type === 'folder' ? -1 : 1
        if (a.sort !== b.sort) return a.sort - b.sort
        return a.name.localeCompare(b.name)
    })
}

async function loadNode(
    node: { level: number; data?: TreeNode },
    resolve: (data: TreeNode[]) => void,
) {
    const parentId = node.level === 0 ? 0 : (node.data?.id ?? 0)
    const parentPath = node.level === 0 ? '' : (node.data?.path ?? '')
    try {
        const list = await DocumentService.ListChildren(parentId)
        const children = (list ?? []).map((n) => toTreeNode(n, parentPath))
        sortTreeNodes(children)
        for (const c of children) {
            nodeCache[c.id] = c
        }
        resolve(children)
    } catch {
        resolve([])
    }
}

const treeData = ref<TreeNode[]>([])

function selectedNode(): TreeNode | null {
    if (selectedId.value == null) return null
    return nodeCache[selectedId.value] ?? null
}

async function reload() {
    reloading.value = true
    try {
        // 清缓存并重新加载根节点
        for (const key of Object.keys(nodeCache)) {
            delete nodeCache[key]
        }
        const list = await DocumentService.ListChildren(0)
        const children = (list ?? []).map((n) => toTreeNode(n, ''))
        sortTreeNodes(children)
        for (const c of children) {
            nodeCache[c.id] = c
        }
        treeData.value = children
        // 重新展开当前选中节点路径
        await nextTick()
    } catch (e: any) {
        ElMessage.error(`加载文档失败：${e?.message || e}`)
    } finally {
        reloading.value = false
    }
}

onMounted(reload)

// ---------- 节点操作 ----------

function createTargetParentId(): number {
    const sel = selectedNode()
    return sel && sel.type === 'folder' ? sel.id : 0
}

function toolbarCreate(type: 'file' | 'folder') {
    void createNode(type, createTargetParentId())
}

async function reloadParent(id: number) {
    const list = await DocumentService.ListChildren(id)
    const parent = id === 0 ? undefined : nodeCache[id]
    const parentPath = parent?.path ?? ''
    const children = (list ?? []).map((n) => toTreeNode(n, parentPath))
    sortTreeNodes(children)
    for (const c of children) {
        nodeCache[c.id] = c
    }
    if (id === 0) {
        treeData.value = children
    } else {
        treeRef.value?.updateKeyChildren(id, children)
    }
}

async function createNode(type: 'file' | 'folder', parentId = 0) {
    const label = type === 'file' ? '新建文件' : '新建文件夹'
    const values = await showInputDialog(label, [{ key: 'name', label: '名称' }])
    if (!values) return
    const name = values.name.trim()
    if (!name) return
    try {
        const created = await DocumentService.Create(parentId, name, type)
        await reloadParent(parentId)
        if (created.type === 'file') {
            await openDocument({ id: created.id, name: created.name, type: 'file', parentId, kind: created.kind })
        } else {
            selectedId.value = created.id
            treeRef.value?.setCurrentKey(created.id)
        }
    } catch (e: any) {
        ElMessage.error(`创建失败：${e?.message || e}`)
    }
}

async function renameSelected() {
    const node = selectedNode()
    if (!node) return
    await renameNode(node)
}

async function renameNode(node: { id: number; name: string; type: string }) {
    const values = await showInputDialog('重命名', [{ key: 'name', label: '新名称', initial: node.name }])
    if (!values) return
    const name = values.name.trim()
    if (!name || name === node.name) return
    try {
        await DocumentService.Rename(node.id, name)
        const cached = nodeCache[node.id]
        const parentId = cached?.parentId ?? 0
        await reloadParent(parentId)
        // 打开的标签被重命名时同步名称 / 类型（如 .txt → .md 切换编辑器类型）
        const tabIdx = tabs.value.findIndex((t) => t.id === node.id)
        if (tabIdx >= 0) {
            const fresh = nodeCache[node.id]
            if (fresh) {
                const old = tabs.value[tabIdx]
                const content = editorRefs.value[old.key]?.getContent() ?? old.original
                const key = String(fresh.id)
                tabs.value.splice(tabIdx, 1, {
                    ...old,
                    key,
                    name: fresh.name,
                    path: fresh.path,
                    kind: fresh.kind,
                    original: content,
                })
                delete editorRefs.value[old.key]
                activeKey.value = key
                await nextTick()
                editorRefs.value[key]?.setContent(content)
            }
        }
    } catch (e: any) {
        ElMessage.error(`重命名失败：${e?.message || e}`)
    }
}

async function deleteSelected() {
    const node = selectedNode()
    if (!node) return
    await deleteNode(node)
}

async function deleteNode(node: { id: number; name: string; type: string }) {
    const tip =
        node.type === 'folder'
            ? `确定删除目录「${node.name}」及其全部内容？`
            : `确定删除文件「${node.name}」？`
    const ok = await showConfirmDialog('删除', tip, true, '删除')
    if (!ok) return
    try {
        const cached = nodeCache[node.id]
        const parentId = cached?.parentId ?? 0
        await DocumentService.Delete(node.id)
        removeTabByDocId(node.id)
        if (selectedId.value === node.id) selectedId.value = null
        delete nodeCache[node.id]
        await reloadParent(parentId)
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

async function reorderNode(id: number, newParentId: number, targetId: number, position: string) {
    try {
        await DocumentService.Reorder(id, newParentId, targetId, position)
        await reloadParent(newParentId)
        // 如果移动到不同父节点，也需要刷新原父节点的子节点
        const cached = nodeCache[id]
        if (cached && cached.parentId !== newParentId) {
            await reloadParent(cached.parentId)
        }
    } catch (e: any) {
        ElMessage.error(`移动失败：${e?.message || e}`)
        await reload()
    }
}

// ---------- 树交互 ----------

function onNodeClick(data: TreeNode) {
    selectedId.value = data.id
    if (data.type === 'file') {
        void openDocument(data)
    }
}

function onNodeContext(event: MouseEvent, data: TreeNode) {
    event.preventDefault()
    event.stopPropagation()
    ctxNode.value = data
    ctxItems.value = buildCtx(data)
    openCtx(event)
}

function onBlankContext(event: MouseEvent) {
    event.preventDefault()
    ctxNode.value = null
    ctxItems.value = buildCtx(null)
    openCtx(event)
}

function buildCtx(data: TreeNode | null): (CtxItem | 'divider')[] {
    if (!data) {
        return [
            { key: 'new-file', label: '新建文件', icon: DocumentAdd },
            { key: 'new-folder', label: '新建文件夹', icon: FolderAdd },
        ]
    }
    const items: (CtxItem | 'divider')[] = []
    if (data.type === 'folder') {
        items.push({ key: 'new-file', label: '新建文件', icon: DocumentAdd })
        items.push({ key: 'new-folder', label: '新建文件夹', icon: FolderAdd })
        items.push('divider')
    }
    items.push({ key: 'rename', label: '重命名', icon: Edit })
    items.push({ key: 'delete', label: '删除', icon: Delete, danger: true })
    return items
}

function openCtx(event: MouseEvent) {
    ctxX.value = event.clientX
    ctxY.value = event.clientY
    ctxVisible.value = false
    requestAnimationFrame(() => {
        ctxVisible.value = true
    })
}

async function onCtxPick(item: CtxItem) {
    const target = ctxNode.value
    switch (item.key) {
        case 'new-file':
            await createNode('file', target && target.type === 'folder' ? target.id : 0)
            break
        case 'new-folder':
            await createNode('folder', target && target.type === 'folder' ? target.id : 0)
            break
        case 'rename':
            if (target) await renameNode(target)
            break
        case 'delete':
            if (target) await deleteNode(target)
            break
    }
}

// ---------- 标签页右键 ----------

function onTabContext(event: MouseEvent, t: DocTab) {
    event.preventDefault()
    tabCtxTab.value = t
    tabCtxIndex.value = tabs.value.findIndex((x) => x.key === t.key)
    tabCtxItems.value = buildTabCtx()
    tabCtxX.value = event.clientX
    tabCtxY.value = event.clientY
    tabCtxVisible.value = false
    requestAnimationFrame(() => {
        tabCtxVisible.value = true
    })
}

function buildTabCtx(): (CtxItem | 'divider')[] {
    const total = tabs.value.length
    const idx = tabCtxIndex.value
    return [
        { key: 'close-current', label: '关闭当前', icon: Close, disabled: total === 0 },
        { key: 'close-others', label: '关闭其他', icon: CloseBold, disabled: total <= 1 },
        { key: 'close-right', label: '关闭右边', icon: DArrowRight, disabled: idx < 0 || idx >= total - 1 },
        { key: 'close-all', label: '关闭全部', icon: CircleClose, disabled: total === 0 },
    ]
}

async function onTabCtxPick(item: CtxItem) {
    const t = tabCtxTab.value
    if (!t) return
    const idx = tabs.value.findIndex((x) => x.key === t.key)
    switch (item.key) {
        case 'close-current':
            await closeTab(t)
            break
        case 'close-others':
            await closeTabList(tabs.value.filter((x) => x.key !== t.key))
            break
        case 'close-right':
            await closeTabList(idx >= 0 ? tabs.value.slice(idx + 1) : [])
            break
        case 'close-all':
            await closeTabList([...tabs.value])
            break
    }
}

async function closeTabList(list: DocTab[]) {
    if (!list.length) return
    const dirtyCount = list.filter((t) => t.dirty).length
    if (dirtyCount > 0) {
        const ok = await showConfirmDialog(
            '关闭文档',
            `有 ${dirtyCount} 个标签存在未保存的更改，确定关闭？`,
            true,
            '不保存并关闭',
        )
        if (!ok) return
    }
    for (const t of list) removeTabByKey(t.key)
}

function allowDrop(dragging: any, drop: any, type: string): boolean {
    const dragId = dragging?.data?.id
    const dropId = drop?.data?.id
    if (dropId === dragId) return false
    if (type === 'inner') {
        return drop?.data?.type === 'folder'
    }
    // before / after：仅允许同类型节点之间排序（文件夹归文件夹、文件归文件）
    return dragging?.data?.type === drop?.data?.type
}

function onNodeDrop(dragging: any, drop: any, dropType: string) {
    const id = dragging.data.id as number
    if (dropType === 'inner') {
        const newParentId = drop.data.id as number
        if (newParentId === id) return
        void reorderNode(id, newParentId, 0, 'after')
        return
    }
    const targetId = drop.data.id as number
    if (targetId === id) return
    const newParentId: number = drop.data.parentId ?? 0
    void reorderNode(id, newParentId, targetId, dropType)
}

// ---------- 多标签编辑器 ----------

function setEditorRef(key: string, el: EditorApi | null) {
    editorRefs.value[key] = el
}

async function openDocument(
    node: { id: number; name: string; type: string; parentId: number; kind?: string },
    lineNo?: number,
) {
    if (node.type !== 'file') return
    const key = String(node.id)
    const existing = tabs.value.find((t) => t.key === key)
    if (existing) {
        // 已打开：激活并定位
        activeKey.value = key
        selectedId.value = node.id
        treeRef.value?.setCurrentKey(node.id)
        leftTab.value = 'tree'
        await nextTick()
        // KeepAlive 可能会因超过 max 缓存数而销毁旧编辑器，切回来时需恢复内容
        const ed = editorRefs.value[key]
        if (ed && !ed.getContent()) {
          ed.setContent(existing.original)
        }
        if (lineNo) {
          ;(editorRefs.value[key] as any)?.jumpToLine?.(lineNo)
        }
        return
    }
    try {
        const content = await DocumentService.GetContent(node.id)
        const kind = node.kind || kindForName(node.name)
        const docPath = nodeCache[node.id]?.path ?? ('/' + node.name)
        const tab: DocTab = {
            key,
            id: node.id,
            name: node.name,
            path: docPath,
            kind,
            original: content,
            dirty: false,
        }
        tabs.value.push(tab)
        activeKey.value = key
        // 等编辑器组件挂载完成后再写入内容
        await nextTick()
        editorRefs.value[key]?.setContent(content)
        if (lineNo) (editorRefs.value[key] as any)?.jumpToLine?.(lineNo)
        else editorRefs.value[key]?.focus()
        selectedId.value = node.id
        treeRef.value?.setCurrentKey(node.id)
        leftTab.value = 'tree'
    } catch (e: any) {
        ElMessage.error(`打开文档失败：${e?.message || e}`)
    }
}

async function closeTab(tab: DocTab) {
    if (tab.dirty) {
        const ok = await showConfirmDialog('关闭文档', `「${tab.name}」有未保存的更改，确定关闭？`, true, '不保存并关闭')
        if (!ok) return
    }
    removeTabByKey(tab.key)
}

// 关闭标签并释放对应编辑器实例
function removeTabByKey(key: string) {
    const idx = tabs.value.findIndex((t) => t.key === key)
    if (idx < 0) return
    tabs.value.splice(idx, 1)
    delete editorRefs.value[key]
    if (activeKey.value === key) {
        activeKey.value = tabs.value[idx]?.key ?? tabs.value[idx - 1]?.key ?? null
    }
}

function removeTabByDocId(id: number) {
    const tab = tabs.value.find((t) => t.id === id)
    if (tab) removeTabByKey(tab.key)
}

function onChange(tab: DocTab, value: string) {
    tab.dirty = value !== tab.original
}

async function save(): Promise<boolean> {
    const tab = activeTab.value
    if (!tab) return true
    const ed = editorRefs.value[tab.key]
    if (!ed) return true
    const content = ed.getContent()
    saving.value = true
    try {
        await DocumentService.SaveContent(tab.id, content)
        tab.original = content
        tab.dirty = false
        ElMessage.success('已保存')
        return true
    } catch (e: any) {
        ElMessage.error(`保存失败：${e?.message || e}`)
        return false
    } finally {
        saving.value = false
    }
}

// ---------- AI 助手 ----------

function markActiveDirty() {
    const tab = activeTab.value
    if (!tab) return
    const ed = editorRefs.value[tab.key]
    if (ed) tab.dirty = ed.getContent() !== tab.original
}

async function onAiCommand(cmd: string) {
    const tab = activeTab.value
    if (!tab) return
    const ed = editorRefs.value[tab.key]
    if (!ed) return
    const full = ed.getContent() ?? ''
    const sel = ed.getSelection?.() ?? ''

    if (cmd === 'summarize') {
        const target = sel || full
        if (!target.trim()) {
            ElMessage.warning('当前文档为空')
            return
        }
        streamToDialog('summarize', target, undefined, { title: '总结' })
    } else if (cmd === 'rewrite') {
        const target = sel || full
        if (!target.trim()) {
            ElMessage.warning('当前文档为空')
            return
        }
        const values = await showInputDialog('改写', [
            { key: 'req', label: '改写要求（可选）', type: 'textarea', optional: true },
        ])
        if (!values) return
        if (sel && ed.replaceSelection) {
            streamToDialog('rewrite-doc', target, values.req.trim(), {
                title: '改写',
                replaceLabel: '替换选中',
                onReplace: (t) => {
                    ed.replaceSelection?.(t)
                    markActiveDirty()
                },
            })
        } else {
            streamToDialog('rewrite-doc', target, values.req.trim(), {
                title: '改写',
                replaceLabel: '替换全文',
                onReplace: (t) => {
                    ed.setContent(t)
                    markActiveDirty()
                },
            })
        }
    } else if (cmd === 'translate') {
        const target = sel || full
        if (!target.trim()) {
            ElMessage.warning('当前文档为空')
            return
        }
        if (sel && ed.replaceSelection) {
            streamToDialog('translate', target, undefined, {
                title: '翻译',
                replaceLabel: '替换选中',
                onReplace: (t) => {
                    ed.replaceSelection?.(t)
                    markActiveDirty()
                },
            })
        } else {
            streamToDialog('translate', target, undefined, {
                title: '翻译',
                replaceLabel: '替换全文',
                onReplace: (t) => {
                    ed.setContent(t)
                    markActiveDirty()
                },
            })
        }
    } else if (cmd === 'generate') {
        const values = await showInputDialog('生成内容', [
            { key: 'desc', label: '描述你想要的内容', type: 'textarea', placeholder: '如：写一段项目 README 简介' },
        ])
        if (!values) return
        streamToDialog('generate-doc', values.desc.trim(), undefined, {
            title: '生成内容',
            insertLabel: '插入到文末',
            onInsert: (t) => {
                ed.setContent(full ? full + '\n' + t : t)
                markActiveDirty()
            },
        })
    }
}

// ---------- 搜索 ----------

async function runSearch() {
    const kw = keyword.value.trim()
    if (!kw) {
        ElMessage.warning('请输入搜索关键字')
        return
    }
    searching.value = true
    try {
        results.value = (await DocumentService.Search(kw, searchMode.value)) ?? []
        leftTab.value = 'search'
    } catch (e: any) {
        ElMessage.error(`搜索失败：${e?.message || e}`)
    } finally {
        searching.value = false
    }
}

function openResult(row: SearchResult) {
    // 优先从缓存取完整节点；否则用搜索结果字段构造最小节点
    const cached = nodeCache[row.id]
    const node = cached ?? {
        id: row.id,
        name: row.name,
        type: 'file',
        parentId: 0,
        kind: row.kind || kindForName(row.name),
    }
    void openDocument(node, row.lineNo)
}
</script>

<style scoped>
.doc-view {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
}

.doc-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    flex-wrap: wrap;
}

.doc-toolbar .el-button+span {
    margin-left: 4px;
}

.toolbar-spacer {
    flex: 1;
}

.doc-body {
    flex: 1;
    min-height: 0;
}

.doc-body :deep(.el-splitter) {
    height: 100%;
}

.doc-body :deep(.el-splitter-panel) {
    min-width: 0;
}

.doc-body :deep(.el-splitter-panel:not(:last-child)) {
    padding-right: 8px;
}

.doc-left {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
}

.left-tabs {
    display: flex;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.left-tab {
    flex: 1;
    text-align: center;
    padding: 8px 0;
    font-size: 12.5px;
    color: var(--text-secondary);
    cursor: pointer;
    user-select: none;
    border-bottom: 2px solid transparent;
}

.left-tab.active {
    color: var(--active-text);
    border-bottom-color: var(--active-text);
}

.tab-badge {
    margin-left: 4px;
    font-size: 11px;
    color: var(--text-secondary);
    background: var(--hover-strong);
    border-radius: 8px;
    padding: 0 6px;
}

.left-tab-action {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    padding: 0 6px;
}

@keyframes spin {
    to { transform: rotate(360deg); }
}

.spinning {
    animation: spin 0.8s linear infinite;
}

.tree-wrap,
.results-wrap {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 4px;
}

.results-wrap {
    padding: 0;
}

.tree-node {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    overflow: hidden;
}

.tree-node-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.md-tag {
    transform: scale(0.8);
    margin-left: 2px;
    flex-shrink: 0;
}

.res-name {
    margin-left: 6px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.dim {
    color: var(--text-secondary);
    font-size: 12px;
}

.doc-right {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
}

/* 多标签栏 */
.doc-tabbar {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 4px 6px 0;
    /* border-bottom: 1px solid var(--border-color); */
    flex-shrink: 0;
    overflow-x: auto;
}

.doc-tab {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 6px 10px;
    font-size: 12.5px;
    color: var(--text-secondary);
    /* background: var(--panel-soft); */
    /* border: 1px solid var(--border-color); */
    border-bottom: none;
    border-radius: 6px 6px 0 0;
    cursor: pointer;
    max-width: 200px;
    flex-shrink: 0;
    user-select: none;
    margin-bottom: -1px;
}

.doc-tab.active {
    background: var(--active-bg);
    color: var(--active-text);
}

.doc-tab-title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.doc-tab-dirty {
    font-size: 10px;
    color: #e6c06c;
    flex-shrink: 0;
}

.doc-tab-close {
    color: var(--text-secondary);
    border-radius: 3px;
    flex-shrink: 0;
}

.doc-tab-close:hover {
    color: #f56c6c;
    background: rgba(245, 108, 108, 0.15);
}

.doc-tab-actions {
    margin-left: auto;
    padding: 0 2px 4px;
    flex-shrink: 0;
}

.editor-area {
    flex: 1;
    min-height: 0;
    position: relative;
}

.doc-pane {
    position: absolute;
    inset: 0;
}

.editor-empty {
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

.editor-empty p {
    margin: 0;
    font-size: 13px;
}

.editor-empty .sub {
    font-size: 12px;
    opacity: 0.7;
}
</style>