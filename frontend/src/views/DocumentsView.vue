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

            <div class="toolbar-spacer" />

            <el-input v-model="keyword" size="small" placeholder="搜索文档（文件名 / 内容）" clearable style="width: 230px"
                @keyup.enter="runSearch" />
            <el-radio-group v-model="searchMode" size="small">
                <el-radio-button value="name">文件名</el-radio-button>
                <el-radio-button value="content">内容</el-radio-button>
            </el-radio-group>
            <el-button size="small" type="primary" :loading="searching" @click="runSearch">搜索</el-button>
        </div>

        <!-- 主体：左树 + 右编辑器 -->
        <div class="doc-body">
            <el-splitter>
                <el-splitter-panel size="26%" :min="200">
                    <div class="doc-left">
                        <div class="left-tabs">
                            <div class="left-tab" :class="{ active: leftTab === 'tree' }" @click="leftTab = 'tree'">文件
                            </div>
                            <div class="left-tab" :class="{ active: leftTab === 'search' }" @click="leftTab = 'search'">
                                搜索<span v-if="results.length" class="tab-badge">{{ results.length }}</span>
                            </div>
                        </div>

                        <div v-show="leftTab === 'tree'" class="tree-wrap" @contextmenu="onBlankContext">
                            <el-tree ref="treeRef" :data="treeData" node-key="id"
                                :props="{ label: 'name', children: 'children' }" highlight-current default-expand-all
                                :expand-on-click-node="true" draggable :allow-drop="allowDrop" @node-click="onNodeClick"
                                @node-contextmenu="onNodeContext" @node-drop="onNodeDrop"
                                empty-text="暂无文档，点击上方「新建文件」开始">
                                <template #default="{ data }">
                                    <span class="tree-node">
                                        <el-icon :color="data.type === 'folder' ? '#e6c06c' : '#8b90a0'">
                                            <Folder v-if="data.type === 'folder'" />
                                            <Document v-else />
                                        </el-icon>
                                        <span class="tree-node-name" :title="data.name">{{ data.name }}</span>
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
                                        <el-icon v-else color="#8b90a0">
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
                        <div class="doc-editor-head">
                            <span class="doc-title" :title="currentPath">{{ currentPath || '未打开文档' }}</span>
                            <span v-if="dirty" class="dirty-tag">● 未保存</span>
                            <div class="head-spacer" />
                            <el-button size="small" type="primary" :loading="saving" :disabled="!currentFile"
                                @click="save">
                                保存
                            </el-button>
                        </div>

                        <div class="editor-area">
                            <CodeEditor ref="editorRef" :filename="currentName" @change="onChange" @save="save" />
                            <div v-if="!currentFile" class="editor-empty">
                                <el-icon :size="40">
                                    <Document />
                                </el-icon>
                                <p>在左侧选择或新建一个文档开始编辑</p>
                                <p class="sub">支持语法高亮、搜索（Ctrl+F）、Ctrl+S 保存</p>
                            </div>
                        </div>
                    </div>
                </el-splitter-panel>
            </el-splitter>
        </div>

        <ContextMenu v-model="ctxVisible" :x="ctxX" :y="ctxY" :items="ctxItems" @pick="onCtxPick" />
    </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import {
    Folder,
    Document,
    FolderAdd,
    DocumentAdd,
    Edit,
    Delete,
} from '@element-plus/icons-vue'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import CodeEditor from '../components/CodeEditor.vue'
import { DocumentService } from '../utils/wails'
import type { DocNode } from '../utils/wails'
import { showInputDialog, showConfirmDialog } from '../utils/dialog'
import type { SearchResult } from '../types'

interface TreeNode {
    id: number
    parentId: number
    name: string
    type: string
    children?: TreeNode[]
}

const nodes = ref<DocNode[]>([])
const treeRef = ref()
const editorRef = ref<InstanceType<typeof CodeEditor>>()

const selectedId = ref<number | null>(null)
const currentFile = ref<{ id: number; name: string } | null>(null)
const currentName = ref('')
const currentPath = ref('')
const dirty = ref(false)
const saving = ref(false)

let originalContent = ''

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

// ---------- 数据加载 / 树构建 ----------

const parentMap = computed(() => {
    const m = new Map<number, DocNode>()
    for (const n of nodes.value) m.set(n.id, n)
    return m
})

function fullPathOf(node: { name: string; parentId: number }): string {
    const parts: string[] = []
    let cur: { name: string; parentId: number } | undefined = node
    while (cur) {
        parts.unshift(cur.name)
        cur = cur.parentId ? parentMap.value.get(cur.parentId) : undefined
    }
    return '/' + parts.join('/')
}

const pathMap = computed(() => {
    const m = new Map<string, DocNode>()
    for (const n of nodes.value) m.set(fullPathOf(n), n)
    return m
})

const treeData = computed<TreeNode[]>(() => {
    const map = new Map<number, TreeNode>()
    for (const n of nodes.value) {
        map.set(n.id, { id: n.id, parentId: n.parentId, name: n.name, type: n.type, children: [] })
    }
    const roots: TreeNode[] = []
    for (const n of nodes.value) {
        const node = map.get(n.id)!
        const parent = n.parentId ? map.get(n.parentId) : undefined
        if (parent) parent.children!.push(node)
        else roots.push(node)
    }
    const sortRec = (arr: TreeNode[]) => {
        arr.sort((a, b) =>
            a.type === b.type ? a.name.localeCompare(b.name) : a.type === 'folder' ? -1 : 1,
        )
        arr.forEach((c) => c.children && sortRec(c.children))
    }
    sortRec(roots)
    return roots
})

function selectedNode(): DocNode | null {
    return nodes.value.find((n) => n.id === selectedId.value) ?? null
}

async function reload() {
    try {
        nodes.value = (await DocumentService.List()) ?? []
    } catch (e: any) {
        ElMessage.error(`加载文档失败：${e?.message || e}`)
    }
    await nextTick()
    expandAll()
}

function expandAll() {
    const tree = treeRef.value as any
    if (tree?.store?.nodesMap) {
        for (const k in tree.store.nodesMap) tree.store.nodesMap[k].expanded = true
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

async function createNode(type: 'file' | 'folder', parentId = 0) {
    const label = type === 'file' ? '新建文件' : '新建文件夹'
    const values = await showInputDialog(label, [{ key: 'name', label: '名称' }])
    if (!values) return
    const name = values.name.trim()
    if (!name) return
    try {
        const created = await DocumentService.Create(parentId, name, type)
        await reload()
        if (created.type === 'file') {
            await openDocument(created)
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
        await reload()
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
        await DocumentService.Delete(node.id)
        if (currentFile.value?.id === node.id) closeDocument()
        if (selectedId.value === node.id) selectedId.value = null
        await reload()
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

async function moveNode(id: number, newParentId: number) {
    try {
        await DocumentService.Move(id, newParentId)
        await reload()
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

function allowDrop(dragging: any, drop: any, type: string): boolean {
    const dragId = dragging?.data?.id
    const dropId = drop?.data?.id
    if (type === 'inner') {
        return drop?.data?.type === 'folder' && dropId !== dragId
    }
    return dropId !== dragId
}

function onNodeDrop(dragging: any, drop: any, dropType: string) {
    const id = dragging.data.id as number
    const newParentId: number = dropType === 'inner' ? drop.data.id : drop.data.parentId ?? 0
    if (newParentId === id) return
    void moveNode(id, newParentId)
}

// ---------- 编辑器 ----------

async function openDocument(
    node: { id: number; name: string; type: string; parentId: number },
    lineNo?: number,
) {
    if (node.type !== 'file') return
    if (currentFile.value && currentFile.value.id !== node.id) {
        const proceed = await ensureSaved()
        if (!proceed) return
    }
    try {
        const content = await DocumentService.GetContent(node.id)
        currentFile.value = { id: node.id, name: node.name }
        currentName.value = node.name
        currentPath.value = fullPathOf(node)
        originalContent = content
        editorRef.value?.setContent(content)
        if (lineNo) editorRef.value?.jumpToLine(lineNo)
        else editorRef.value?.focus()
        selectedId.value = node.id
        treeRef.value?.setCurrentKey(node.id)
        leftTab.value = 'tree'
    } catch (e: any) {
        ElMessage.error(`打开文档失败：${e?.message || e}`)
    }
}

function closeDocument() {
    currentFile.value = null
    currentName.value = ''
    currentPath.value = ''
    dirty.value = false
    editorRef.value?.setContent('')
}

function onChange(value: string) {
    if (currentFile.value) {
        dirty.value = value !== originalContent
    }
}

async function save(): Promise<boolean> {
    if (!currentFile.value || !editorRef.value) return true
    const content = editorRef.value.getContent()
    saving.value = true
    try {
        await DocumentService.SaveContent(currentFile.value.id, content)
        originalContent = content
        dirty.value = false
        ElMessage.success('已保存')
        return true
    } catch (e: any) {
        ElMessage.error(`保存失败：${e?.message || e}`)
        return false
    } finally {
        saving.value = false
    }
}

async function ensureSaved(): Promise<boolean> {
    if (!dirty.value || !currentFile.value) return true
    const ok = await showConfirmDialog('未保存的更改', '当前文档有未保存的更改，是否先保存？', false, '保存')
    if (ok) return save()
    return true // 不保存，丢弃更改
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
    const node = pathMap.value.get(row.path)
    if (!node) {
        ElMessage.warning('找不到对应文档')
        return
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
    color: #7fb0ff;
    border-bottom-color: #7fb0ff;
}

.tab-badge {
    margin-left: 4px;
    font-size: 11px;
    color: var(--text-secondary);
    background: #2e3442;
    border-radius: 8px;
    padding: 0 6px;
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

.doc-editor-head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.doc-title {
    font-family: var(--term-font);
    font-size: 12.5px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
}

.dirty-tag {
    font-size: 12px;
    color: #e6c06c;
    flex-shrink: 0;
}

.head-spacer {
    flex: 1;
}

.editor-area {
    flex: 1;
    min-height: 0;
    position: relative;
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
    background: #1a1d24;
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
