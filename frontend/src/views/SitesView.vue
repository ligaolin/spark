<template>
    <div class="sites-view" :class="{ 'is-fullscreen': browserFullscreen }">
        <!-- 顶部工具栏 -->
        <div v-if="!browserFullscreen" class="sites-toolbar">
            <el-button size="small" type="primary" plain @click="toolbarCreate('folder')">
                <el-icon>
                    <FolderAdd />
                </el-icon><span>新建文件夹</span>
            </el-button>
            <el-button size="small" plain @click="toolbarCreate('site')">
                <el-icon>
                    <Plus />
                </el-icon><span>新建站点</span>
            </el-button>
            <span class="hint">站点可在文件夹间拖拽；点击链接右侧 ↗ 在右侧内嵌打开，多站点标签切换</span>
            <div class="toolbar-spacer" />
            <el-switch v-model="ignoreCert" size="small" />
            <span class="ignore-cert-label" :class="{ active: ignoreCert }" title="开启后，SSL 证书无效 / 自签名 / 过期的站点也能在内嵌浏览器中打开（走本地代理，绕过证书校验）">
                忽略证书
            </span>
        </div>

        <div class="sites-body">
            <el-splitter>
                <!-- 左侧树 -->
                <el-splitter-panel v-if="!browserFullscreen" size="24%" :min="200">
                    <div class="tree-pane" @contextmenu="onBlankContext">
                        <el-tree ref="treeRef" :data="treeData" node-key="key"
                            :props="{ label: 'name', children: 'children' }" highlight-current default-expand-all
                            :expand-on-click-node="true" draggable :allow-drop="allowDrop" @node-click="onNodeClick"
                            @node-contextmenu="onNodeContext" @node-drop="onNodeDrop" empty-text="暂无站点，点击上方「新建站点」">
                            <template #default="{ data }">
                                <span class="tree-node">
                                    <el-icon :color="data.type === 'folder' ? '#e6c06c' : 'var(--active-text)'">
                                        <Folder v-if="data.type === 'folder'" />
                                        <Collection v-else />
                                    </el-icon>
                                    <span class="tree-node-name" :title="data.name">{{ data.name }}</span>
                                </span>
                            </template>
                        </el-tree>
                    </div>
                </el-splitter-panel>

                <!-- 右侧：站点详情 / 内嵌浏览器 -->
                <el-splitter-panel :min="200">
                    <div class="right-pane">
                        <div v-if="browserTabs.length" class="tab-bar">
                            <div v-for="tab in browserTabs" :key="tab.key" class="tab"
                                :class="{ active: tab.key === activeTabKey }" @click="activeTabKey = tab.key">
                                <el-icon class="tab-action" @click.stop="copyText(tab.url, '链接')" title="复制链接">
                                    <Link />
                                </el-icon>
                                <span class="tab-title" :title="tab.title">{{ tab.title }}</span>
                                <el-icon class="tab-action" @click.stop="openInSystemBrowser(tab.url)" title="在浏览器打开">
                                    <TopRight />
                                </el-icon>
                                <el-icon class="tab-close" @click.stop="closeTab(tab.key)" title="关闭">
                                    <Close />
                                </el-icon>
                            </div>
                            <el-button size="small" text class="fullscreen-btn"
                                :class="{ active: browserFullscreen }"
                                :title="browserFullscreen ? '退出全屏（Esc）' : '全屏（隐藏左侧菜单）'" @click="toggleFullscreen">
                                <el-icon>
                                    <FullScreen />
                                </el-icon>
                            </el-button>
                        </div>

                        <!-- 站点详情 -->
                        <div v-show="activeTabKey === null" class="site-detail">
                            <template v-if="selectedSite">
                                <div class="detail-head">
                                    <div class="detail-title">
                                        <el-icon color="var(--active-text)">
                                            <Collection />
                                        </el-icon>
                                        <span>{{ selectedSite.name }}</span>
                                    </div>
                                    <div v-if="selectedSite.note" class="detail-note">{{ selectedSite.note }}</div>
                                </div>
                                <div class="detail-body">
                                    <!-- 链接列 -->
                                    <div class="detail-col">
                                        <div class="col-head">
                                            <span>链接</span>
                                            <el-button size="small" text type="primary" @click="createLink">
                                                <el-icon>
                                                    <Plus />
                                                </el-icon><span>添加链接</span>
                                            </el-button>
                                        </div>
                                        <div class="col-list">
                                            <div v-for="l in links" :key="l.id" class="link-card"
                                                :class="{ active: selectedLinkId === l.id }" @click="selectLink(l.id)">
                                                <div class="lc-title" :title="l.name">{{ l.name }}</div>
                                                <div class="lc-url mono" :title="l.url">{{ l.url }}</div>
                                                <div v-if="l.note" class="lc-note" :title="l.note">{{ l.note }}</div>
                                                <div class="lc-actions">
                                                    <el-button size="small" type="primary"
                                                        @click.stop="openLink(l)">打开</el-button>
                                                    <el-button size="small" text @click.stop="openLinkInBrowser(l)"
                                                        title="在系统默认浏览器中打开">浏览器打开</el-button>
                                                    <el-button size="small" text @click.stop="editLink(l)"
                                                        title="编辑"><el-icon>
                                                            <Edit />
                                                        </el-icon></el-button>
                                                    <el-button size="small" text type="danger"
                                                        @click.stop="deleteLink(l)" title="删除"><el-icon>
                                                            <Delete />
                                                        </el-icon></el-button>
                                                </div>
                                            </div>
                                            <div v-if="links.length === 0" class="empty">暂无链接</div>
                                        </div>
                                    </div>
                                    <!-- 账号列（右侧直接展开） -->
                                    <div class="detail-col">
                                        <div class="col-head">
                                            <span>账号</span>
                                            <el-button size="small" text type="primary" :disabled="!selectedLinkId"
                                                @click="createAccount">
                                                <el-icon>
                                                    <Plus />
                                                </el-icon><span>添加账号</span>
                                            </el-button>
                                        </div>
                                        <div class="col-list">
                                            <div v-for="a in linkAccounts" :key="a.id" class="account-card">
                                                <div class="acc-row">
                                                    <span class="acc-label">账号</span>
                                                    <span class="acc-value" :title="a.username">{{ a.username }}</span>
                                                    <el-button size="small" text @click="copyText(a.username, '账号')"
                                                        title="复制账号"><el-icon>
                                                            <CopyDocument />
                                                        </el-icon></el-button>
                                                </div>
                                                <div class="acc-row">
                                                    <span class="acc-label">密码</span>
                                                    <span class="acc-value mono">{{ showPass[a.id] ? a.password :
                                                        '••••••••' }}</span>
                                                    <el-button size="small" text @click="togglePass(a.id)"
                                                        :title="showPass[a.id] ? '隐藏' : '显示'">
                                                        <el-icon>
                                                            <Hide v-if="showPass[a.id]" />
                                                            <View v-else />
                                                        </el-icon>
                                                    </el-button>
                                                    <el-button size="small" text @click="copyText(a.password, '密码')"
                                                        title="复制密码"><el-icon>
                                                            <CopyDocument />
                                                        </el-icon></el-button>
                                                </div>
                                                <div v-if="a.note" class="acc-note" :title="a.note">{{ a.note }}</div>
                                                <div class="acc-actions">
                                                    <el-button size="small" text @click="editAccount(a)">编辑</el-button>
                                                    <el-button size="small" text type="danger"
                                                        @click="deleteAccount(a)">删除</el-button>
                                                </div>
                                            </div>
                                            <div v-if="!selectedLinkId" class="empty">请先在左侧选择链接</div>
                                            <div v-else-if="linkAccounts.length === 0" class="empty">暂无账号</div>
                                        </div>
                                    </div>
                                </div>
                            </template>
                            <div v-else class="detail-empty">
                                <el-icon :size="36">
                                    <Collection />
                                </el-icon>
                                <p>在左侧选择或新建一个站点</p>
                            </div>
                        </div>

                        <!-- 内嵌浏览器 -->
                        <div v-show="activeTabKey !== null" class="browser-area">
                            <div class="browser-frames">
                                <div v-for="tab in browserTabs" :key="tab.key" v-show="tab.key === activeTabKey"
                                    class="frame-wrap">
                                    <!-- 忽略证书时：代理地址解析完成前不挂载 iframe，
                                         避免先用原始地址加载（证书错误）再切换导致空白/残留错误页 -->
                                    <iframe v-if="frameSrc(tab.url)" :src="frameSrc(tab.url)" class="browser-frame" />
                                    <div v-else-if="ignoreCert && !proxyError[tab.url]" class="frame-loading">
                                        <el-icon class="is-loading" :size="22"><Loading /></el-icon>
                                        <span>正在通过本地代理打开（忽略证书）…</span>
                                    </div>
                                    <div v-else-if="ignoreCert && proxyError[tab.url]" class="frame-loading">
                                        <el-icon :size="22" color="#f56c6c"><CircleCloseFilled /></el-icon>
                                        <span>代理打开失败：{{ proxyError[tab.url] }}</span>
                                        <el-button size="small" type="primary" @click="openInSystemBrowser(tab.url)">
                                            在系统浏览器打开
                                        </el-button>
                                    </div>
                                </div>
                            </div>
                            <div v-if="ignoreCert" class="proxy-badge">
                                已开启「忽略证书」：内嵌页面经本地代理打开，可访问 SSL 有问题的站点
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
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Clipboard } from '@wailsio/runtime'
import {
    Collection,
    Link,
    Edit,
    Delete,
    CopyDocument,
    View,
    Hide,
    Plus,
    Folder,
    FolderAdd,
    Close,
    TopRight,
    FullScreen,
    Loading,
    CircleCloseFilled,
} from '@element-plus/icons-vue'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import { SiteService } from '../utils/wails'
import type { Site, SiteLink, SiteAccount, SiteFolder } from '../utils/wails'
import { showInputDialog, showConfirmDialog } from '../utils/dialog'

interface SiteTreeNode {
    key: string
    id: number
    parentId: number // 所在文件夹 id（0 = 根）
    name: string
    type: 'folder' | 'site'
    children?: SiteTreeNode[]
}

interface BrowserTab {
    key: string
    title: string
    url: string
}

const folders = ref<SiteFolder[]>([])
const sites = ref<Site[]>([])
const links = ref<SiteLink[]>([])

const treeRef = ref()
const selectedSiteId = ref<number | null>(null)
const selectedLinkId = ref<number | null>(null)
const linkAccounts = ref<SiteAccount[]>([])

const browserTabs = ref<BrowserTab[]>([])
const activeTabKey = ref<string | null>(null)
const browserFullscreen = ref(false)
let tabSeq = 0

// 忽略证书：开启后内嵌浏览器走本地代理（后端忽略 TLS 证书校验抓取页面），
// SSL 证书无效 / 自签名 / 过期的站点也能打开。
const ignoreCert = ref(false)
// 原始 URL -> 代理 URL 缓存（ProxyUrl 为异步调用，解析前不挂载 iframe）
const proxyCache = reactive<Record<string, string>>({})
// 原始 URL -> 代理启动失败原因（打开失败时给出错误提示与浏览器打开兜底）
const proxyError = reactive<Record<string, string>>({})

// 代理地址解析完成后才返回（未就绪返回 ''，模板据此不渲染 iframe）
function frameSrc(raw: string): string {
    if (!ignoreCert.value) return raw
    return proxyCache[raw] || ''
}

// 主动解析代理地址（开启忽略证书后立即解析所有已打开标签，切换标签即时可用）
function resolveProxy(raw: string) {
    if (!ignoreCert.value || proxyCache[raw] || proxyError[raw]) return
    SiteService.ProxyUrl(raw)
        .then((u) => {
            if (u) proxyCache[raw] = u
        })
        .catch((e: any) => {
            proxyError[raw] = e?.message || String(e)
        })
}

watch(
    () => [ignoreCert.value, browserTabs.value.map((t) => t.url)] as [boolean, string[]],
    () => {
        if (!ignoreCert.value) return
        for (const url of browserTabs.value.map((t) => t.url)) resolveProxy(url)
    },
    { immediate: true },
)

const showPass = reactive<Record<number, boolean>>({})

// 右键菜单
const ctxVisible = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)
const ctxItems = ref<(CtxItem | 'divider')[]>([])
const ctxNode = ref<SiteTreeNode | null>(null)

const selectedSite = computed(() => sites.value.find((s) => s.id === selectedSiteId.value) ?? null)

// ---------- 树构建 ----------

const treeData = computed<SiteTreeNode[]>(() => {
    const map = new Map<string, SiteTreeNode>()
    for (const f of folders.value) {
        map.set('f:' + f.id, { key: 'f:' + f.id, id: f.id, parentId: f.parentId, name: f.name, type: 'folder', children: [] })
    }
    for (const s of sites.value) {
        map.set('s:' + s.id, { key: 's:' + s.id, id: s.id, parentId: s.folderId, name: s.name, type: 'site', children: [] })
    }
    const roots: SiteTreeNode[] = []
    for (const n of map.values()) {
        const parentKey = n.parentId ? 'f:' + n.parentId : ''
        const parent = parentKey ? map.get(parentKey) : undefined
        if (parent) parent.children!.push(n)
        else roots.push(n)
    }
    const sortRec = (arr: SiteTreeNode[]) => {
        arr.sort((a, b) =>
            a.type === b.type ? a.name.localeCompare(b.name) : a.type === 'folder' ? -1 : 1,
        )
        arr.forEach((c) => c.children && sortRec(c.children))
    }
    sortRec(roots)
    return roots
})

async function loadAll() {
    try {
        folders.value = (await SiteService.ListFolders()) ?? []
        sites.value = (await SiteService.ListSites()) ?? []
    } catch (e: any) {
        ElMessage.error(`加载失败：${e?.message || e}`)
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

async function loadLinks(siteId: number) {
    try {
        links.value = (await SiteService.ListLinks(siteId)) ?? []
    } catch (e: any) {
        ElMessage.error(`加载链接失败：${e?.message || e}`)
    }
    if (links.value.length) {
        selectLink(links.value[0].id)
    } else {
        selectedLinkId.value = null
        linkAccounts.value = []
    }
}

async function loadLinkAccounts(linkId: number) {
    try {
        linkAccounts.value = (await SiteService.ListAccounts(linkId)) ?? []
        for (const a of linkAccounts.value) {
            if (showPass[a.id] === undefined) showPass[a.id] = false
        }
    } catch (e: any) {
        ElMessage.error(`加载账号失败：${e?.message || e}`)
    }
}

onMounted(loadAll)

// 全屏时导航菜单被盖住，按 Esc 退出全屏作为兜底出口
// （焦点在内嵌 iframe 内时按键不会冒泡到这里，此时用标签栏右侧的全屏按钮退出）
function onEsc(e: KeyboardEvent) {
    if (e.key === 'Escape' && browserFullscreen.value) {
        e.preventDefault()
        browserFullscreen.value = false
    }
}

onMounted(() => window.addEventListener('keydown', onEsc))
onBeforeUnmount(() => window.removeEventListener('keydown', onEsc))

// ---------- 树交互 ----------

function onNodeClick(data: SiteTreeNode) {
    if (data.type === 'site') {
        selectSite(data.id)
    }
}

function selectSite(id: number) {
    selectedSiteId.value = id
    activeTabKey.value = null // 回到站点详情
    selectedLinkId.value = null
    linkAccounts.value = []
    treeRef.value?.setCurrentKey('s:' + id)
    void loadLinks(id)
}

function selectLink(id: number) {
    selectedLinkId.value = id
    void loadLinkAccounts(id)
}

function onNodeContext(event: MouseEvent, data: SiteTreeNode) {
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

function buildCtx(data: SiteTreeNode | null): (CtxItem | 'divider')[] {
    if (!data) {
        return [
            { key: 'new-folder', label: '新建文件夹', icon: FolderAdd },
            { key: 'new-site', label: '新建站点', icon: Plus },
        ]
    }
    if (data.type === 'folder') {
        return [
            { key: 'new-folder', label: '新建文件夹', icon: FolderAdd },
            { key: 'new-site', label: '新建站点', icon: Plus },
            'divider',
            { key: 'rename', label: '重命名', icon: Edit },
            { key: 'delete', label: '删除', icon: Delete, danger: true },
        ]
    }
    return [
        { key: 'edit-site', label: '编辑站点', icon: Edit },
        { key: 'delete', label: '删除', icon: Delete, danger: true },
    ]
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
        case 'new-folder':
            await createFolder(target && target.type === 'folder' ? target.id : 0)
            break
        case 'new-site':
            await createSite(target && target.type === 'folder' ? target.id : 0)
            break
        case 'rename':
            if (target) await renameNode(target)
            break
        case 'edit-site':
            if (target) await editSite(target)
            break
        case 'delete':
            if (target) await deleteNode(target)
            break
    }
}

function toolbarCreate(type: 'folder' | 'site') {
    if (type === 'folder') void createFolder(0)
    else void createSite(0)
}

async function createFolder(parentFolderId = 0) {
    const v = await showInputDialog('新建文件夹', [{ key: 'name', label: '文件夹名称' }])
    if (!v) return
    const name = v.name.trim()
    if (!name) return
    try {
        await SiteService.CreateFolder(parentFolderId, name)
        await loadAll()
    } catch (e: any) {
        ElMessage.error(`创建失败：${e?.message || e}`)
    }
}

async function createSite(parentFolderId = 0) {
    const v = await showInputDialog('新建站点', [
        { key: 'name', label: '站点名称' },
        { key: 'note', label: '备注', optional: true },
    ])
    if (!v) return
    try {
        await SiteService.CreateSite(parentFolderId, v.name.trim(), v.note.trim())
        await loadAll()
    } catch (e: any) {
        ElMessage.error(`创建失败：${e?.message || e}`)
    }
}

async function renameNode(node: SiteTreeNode) {
    const v = await showInputDialog('重命名', [{ key: 'name', label: '名称', initial: node.name }])
    if (!v) return
    const name = v.name.trim()
    if (!name || name === node.name) return
    try {
        if (node.type === 'folder') await SiteService.RenameFolder(node.id, name)
        else {
            const site = sites.value.find((s) => s.id === node.id)
            await SiteService.UpdateSite(node.id, name, site?.note ?? '')
        }
        await loadAll()
    } catch (e: any) {
        ElMessage.error(`重命名失败：${e?.message || e}`)
    }
}

async function editSite(node: SiteTreeNode) {
    const site = sites.value.find((s) => s.id === node.id)
    const v = await showInputDialog('编辑站点', [
        { key: 'name', label: '站点名称', initial: site?.name ?? node.name },
        { key: 'note', label: '备注', initial: site?.note ?? '', optional: true },
    ])
    if (!v) return
    try {
        await SiteService.UpdateSite(node.id, v.name.trim(), v.note.trim())
        await loadAll()
    } catch (e: any) {
        ElMessage.error(`保存失败：${e?.message || e}`)
    }
}

async function deleteNode(node: SiteTreeNode) {
    const tip =
        node.type === 'folder'
            ? `确定删除文件夹「${node.name}」及其中的站点？`
            : `确定删除站点「${node.name}」及其链接与账号？`
    const ok = await showConfirmDialog('删除', tip, true, '删除')
    if (!ok) return
    try {
        if (node.type === 'folder') {
            await SiteService.DeleteFolder(node.id)
        } else {
            await SiteService.DeleteSite(node.id)
            if (selectedSiteId.value === node.id) {
                selectedSiteId.value = null
                links.value = []
                selectedLinkId.value = null
                linkAccounts.value = []
            }
        }
        await loadAll()
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

function allowDrop(dragging: any, drop: any, type: string): boolean {
    const dragId = dragging?.data?.id
    const dropId = drop?.data?.id
    const dropType = drop?.data?.type
    if (type === 'inner') {
        return dropType === 'folder' && dropId !== dragId
    }
    return dropId !== dragId
}

function onNodeDrop(dragging: any, drop: any, dropType: string) {
    const node = dragging.data as SiteTreeNode
    const newFolderId: number = dropType === 'inner' ? drop.data.id : drop.data.parentId ?? 0
    if (node.type === 'folder') {
        if (newFolderId === node.id) return
        void moveFolder(node.id, newFolderId)
    } else {
        void moveSite(node.id, newFolderId)
    }
}

async function moveFolder(id: number, newParentId: number) {
    try {
        await SiteService.MoveFolder(id, newParentId)
        await loadAll()
    } catch (e: any) {
        ElMessage.error(`移动失败：${e?.message || e}`)
        await loadAll()
    }
}

async function moveSite(id: number, newFolderId: number) {
    try {
        await SiteService.MoveSite(id, newFolderId)
        await loadAll()
    } catch (e: any) {
        ElMessage.error(`移动失败：${e?.message || e}`)
        await loadAll()
    }
}

// ---------- 链接 ----------

async function createLink() {
    if (!selectedSiteId.value) return
    const v = await showInputDialog('添加链接', [
        { key: 'name', label: '链接名称（可留空）', optional: true },
        { key: 'url', label: '地址（URL）', placeholder: 'https://example.com' },
        { key: 'note', label: '备注', optional: true },
    ])
    if (!v) return
    try {
        await SiteService.CreateLink(selectedSiteId.value, v.name.trim(), v.url.trim(), v.note.trim())
        await loadLinks(selectedSiteId.value)
    } catch (e: any) {
        ElMessage.error(`添加失败：${e?.message || e}`)
    }
}

async function editLink(link: SiteLink) {
    const v = await showInputDialog('编辑链接', [
        { key: 'name', label: '链接名称（可留空）', initial: link.name, optional: true },
        { key: 'url', label: '地址（URL）', initial: link.url },
        { key: 'note', label: '备注', initial: link.note, optional: true },
    ])
    if (!v) return
    try {
        await SiteService.UpdateLink(link.id, v.name.trim(), v.url.trim(), v.note.trim())
        if (selectedSiteId.value) await loadLinks(selectedSiteId.value)
    } catch (e: any) {
        ElMessage.error(`保存失败：${e?.message || e}`)
    }
}

async function deleteLink(link: SiteLink) {
    const ok = await showConfirmDialog('删除链接', `确定删除链接「${link.name}」及其账号？`, true, '删除')
    if (!ok) return
    try {
        await SiteService.DeleteLink(link.id)
        if (selectedLinkId.value === link.id) {
            selectedLinkId.value = null
            linkAccounts.value = []
        }
        if (selectedSiteId.value) await loadLinks(selectedSiteId.value)
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

// ---------- 账号（属于链接） ----------

async function createAccount() {
    if (!selectedLinkId.value) return
    const v = await showInputDialog('添加账号', [
        { key: 'username', label: '账号' },
        { key: 'password', label: '密码', type: 'password' },
        { key: 'note', label: '备注', optional: true },
    ])
    if (!v) return
    try {
        await SiteService.CreateAccount(selectedLinkId.value, v.username.trim(), v.password, v.note.trim())
        await loadLinkAccounts(selectedLinkId.value)
    } catch (e: any) {
        ElMessage.error(`添加失败：${e?.message || e}`)
    }
}

async function editAccount(acc: SiteAccount) {
    const v = await showInputDialog('编辑账号', [
        { key: 'username', label: '账号', initial: acc.username },
        { key: 'password', label: '密码', type: 'password', initial: acc.password },
        { key: 'note', label: '备注', initial: acc.note, optional: true },
    ])
    if (!v) return
    try {
        await SiteService.UpdateAccount(acc.id, v.username.trim(), v.password, v.note.trim())
        if (selectedLinkId.value) await loadLinkAccounts(selectedLinkId.value)
    } catch (e: any) {
        ElMessage.error(`保存失败：${e?.message || e}`)
    }
}

async function deleteAccount(acc: SiteAccount) {
    const ok = await showConfirmDialog('删除账号', `确定删除账号「${acc.username}」？`, true, '删除')
    if (!ok) return
    try {
        await SiteService.DeleteAccount(acc.id)
        if (selectedLinkId.value) await loadLinkAccounts(selectedLinkId.value)
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

function togglePass(id: number) {
    showPass[id] = !showPass[id]
}

async function copyText(text: string, label = '') {
    try {
        await Clipboard.SetText(text)
        ElMessage.success(label ? `已复制${label}` : '已复制')
    } catch (e: any) {
        ElMessage.error(`复制失败：${e?.message || e}`)
    }
}

// ---------- 内嵌浏览器 ----------

function normalizeUrl(u: string): string {
    u = (u || '').trim()
    if (!u) return ''
    if (!/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(u)) {
        u = 'https://' + u
    }
    return u
}

function openLink(link: SiteLink) {
    openTab(link.name || link.url, normalizeUrl(link.url))
}

function openLinkInBrowser(link: SiteLink) {
    void openInSystemBrowser(link.url)
}

function openTab(title: string, url: string) {
    const existing = browserTabs.value.find((t) => t.url === url)
    if (existing) {
        activeTabKey.value = existing.key
        return
    }
    const key = 'tab-' + ++tabSeq
    browserTabs.value.push({ key, title, url })
    activeTabKey.value = key
}

function closeTab(key: string) {
    const idx = browserTabs.value.findIndex((t) => t.key === key)
    if (idx < 0) return
    browserTabs.value.splice(idx, 1)
    if (activeTabKey.value === key) {
        activeTabKey.value =
            browserTabs.value[idx]?.key ?? browserTabs.value[idx - 1]?.key ?? null
    }
    // 没有标签可显示时退出全屏
    if (browserTabs.value.length === 0) {
        browserFullscreen.value = false
    }
}

function toggleFullscreen() {
    browserFullscreen.value = !browserFullscreen.value
}

async function openInSystemBrowser(url?: string) {
    if (!url) return
    try {
        await SiteService.OpenInBrowser(url)
    } catch (e: any) {
        ElMessage.error(`打开失败：${e?.message || e}`)
    }
}
</script>

<style scoped>
.sites-view {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
}

/* 全屏：脱离文档流盖住整个窗口（含最左侧导航菜单），拿到最大可视区域。
   z-index 低于 ElementPlus 弹层(2000+) 和 ContextMenu(3000)，弹窗仍能正常盖在上面。 */
.sites-view.is-fullscreen {
    position: fixed;
    inset: 0;
    z-index: 1000;
    gap: 0;
    padding: 0;
    background: var(--app-bg);
}

.sites-view.is-fullscreen .right-pane {
    border: none;
    border-radius: 0;
}

.sites-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    flex-wrap: wrap;
}

.sites-toolbar .el-button+span {
    margin-left: 4px;
}

.toolbar-spacer {
    flex: 1;
}

.ignore-cert-label {
    font-size: 12px;
    color: var(--text-secondary);
    white-space: nowrap;
}

.ignore-cert-label.active {
    color: #e6a23c;
}

.proxy-badge {
    position: absolute;
    left: 12px;
    bottom: 10px;
    z-index: 10;
    font-size: 11.5px;
    color: #e6a23c;
    background: rgba(230, 162, 60, 0.12);
    border: 1px solid rgba(230, 162, 60, 0.4);
    border-radius: 4px;
    padding: 3px 8px;
    pointer-events: none;
}

.browser-area {
    position: relative;
}

.hint {
    font-size: 12px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.sites-body {
    flex: 1;
    min-height: 0;
}

.sites-body :deep(.el-splitter) {
    height: 100%;
}

.sites-body :deep(.el-splitter-panel) {
    min-width: 0;
}

.sites-body :deep(.el-splitter-panel:not(:last-child)) {
    padding-right: 8px;
}

.tree-pane {
    height: 100%;
    overflow: auto;
    padding: 4px;
    background: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 6px;
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

.right-pane {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--panel-bg);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
}

.tab-bar {
    display: flex;
    align-items: stretch;
    gap: 2px;
    padding: 4px 6px 0;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
    overflow-x: auto;
}

.tab {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    font-size: 12.5px;
    color: var(--text-secondary);
    background: var(--panel-soft);
    border: 1px solid var(--border-color);
    border-bottom: none;
    border-radius: 6px 6px 0 0;
    cursor: pointer;
    max-width: 200px;
    flex-shrink: 0;
}

.tab.active {
    background: var(--active-bg);
    color: var(--active-text);
}

.tab-title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.tab-action,
.tab-close {
    color: var(--text-secondary);
    border-radius: 3px;
    flex-shrink: 0;
}

.tab-action:hover {
    color: var(--active-text);
    background: rgba(127, 176, 255, 0.15);
}

.tab-close:hover {
    color: #f56c6c;
    background: rgba(245, 108, 108, 0.15);
}

.fullscreen-btn {
    margin-left: auto;
    align-self: center;
    flex-shrink: 0;
    color: var(--text-secondary);
    border-radius: 4px;
}

.fullscreen-btn:hover,
.fullscreen-btn.active {
    color: var(--active-text);
    background: rgba(127, 176, 255, 0.15);
}

.site-detail {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.detail-head {
    display: flex;
    align-items: baseline;
    gap: 12px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.detail-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 15px;
    font-weight: 600;
    color: var(--text-primary);
}

.detail-note {
    font-size: 12.5px;
    color: var(--text-secondary);
}

.detail-body {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 12px;
    padding: 12px 16px;
}

.detail-col {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    overflow: hidden;
}

.col-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-color);
    font-size: 12.5px;
    font-weight: 600;
    color: var(--text-primary);
    flex-shrink: 0;
}

.col-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.empty {
    padding: 20px 10px;
    text-align: center;
    font-size: 12px;
    color: var(--text-secondary);
}

.link-card,
.account-card {
    padding: 8px 10px;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    display: flex;
    flex-direction: column;
    gap: 12px;
}

.link-card {
    cursor: pointer;
}

.link-card:hover {
    border-color: var(--border-strong);
}

.link-card.active {
    border-color: #5b9dff;
    background: var(--card-active-bg);
}

.lc-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.lc-url {
    font-size: 11.5px;
    color: var(--active-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.lc-note,
.acc-note {
    font-size: 12px;
    color: var(--text-secondary);
    word-break: break-all;
}

.lc-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
}

.account-card {
    background: var(--hover-bg);
}

.acc-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 4px;
    border-top: 1px solid var(--border-color);
    padding-top: 4px;
    margin-top: 2px;
}

.acc-row {
    display: flex;
    align-items: center;
    gap: 8px;
}

.acc-label {
    width: 34px;
    flex-shrink: 0;
    font-size: 12px;
    color: var(--text-secondary);
}

.acc-value {
    flex: 1;
    min-width: 0;
    font-size: 13px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.mono {
    font-family: var(--term-font);
}

.detail-empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--text-secondary);
}

.detail-empty p {
    margin: 0;
    font-size: 13px;
}

.browser-area {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.browser-frames {
    flex: 1;
    min-height: 0;
    position: relative;
}

.frame-wrap {
    position: absolute;
    inset: 0;
}

.frame-loading {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    background: var(--panel-soft);
    color: var(--text-secondary);
    font-size: 12.5px;
}

.browser-frame {
    width: 100%;
    height: 100%;
    border: none;
    background: #fff;
}
</style>
