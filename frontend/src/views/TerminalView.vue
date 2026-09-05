<template>
    <div class="terminal-view">
        <div v-if="store.tabs.length === 0" class="empty-state">
            <el-icon :size="44" color="var(--border-strong)">
                <Monitor />
            </el-icon>
            <p>还没有 SSH 会话</p>
            <el-button type="primary" @click="openDialog">新建 SSH 会话</el-button>
        </div>

        <template v-else>
            <div class="tab-container-container">
                <div class="tab-bar">
                    <div v-for="tab in store.tabs" :key="tab.key" class="tab"
                        :class="{ active: tab.key === store.activeKey }" @click="store.setActive(tab.key)"
                        @auxclick="onMiddleClick(tab.key, $event)" @contextmenu.prevent="onTabContext($event, tab)">
                        <span class="tab-dot" :class="tab.status"></span>
                        <span class="tab-title" :title="tab.title">{{ tab.title }}</span>
                        <el-icon class="tab-close" @click.stop="store.removeTab(tab.key)">
                            <Close />
                        </el-icon>
                    </div>
                    <div class="tab-add" title="新建会话" @click="openDialog">
                        <el-icon>
                            <Plus />
                        </el-icon>
                    </div>
                </div>
                <div class="tab-add" :class="{ active: panelVisible }"
                    :title="panelVisible ? '收起信息面板' : '展开信息面板（SFTP / 信息 / 进程 / 命令 / 网络 / 转发代理）'"
                    @click="panelVisible = !panelVisible">
                    <el-icon>
                        <InfoFilled />
                    </el-icon>
                </div>
            </div>

            <div class="terminal-body">
                <div class="term-area">
                    <TerminalPane v-for="tab in store.tabs" v-show="tab.key === store.activeKey" :key="tab.key"
                        :tab="tab" />
                </div>

                <!-- v-show（而非 v-if）：收起面板时 SFTP 面板保持挂载、连接不断 -->
                <aside v-show="panelVisible" class="side-panel" :class="{ resizing: resizingPanel }"
                    :style="{ width: panelWidth + 'px' }">
                    <div class="resize-handle" title="拖拽调整宽度" @mousedown="startResize" />
                    <div class="side-head">
                        <el-radio-group v-model="panelTab" size="small">
                            <!-- SFTP 文件放在最左侧（服务信息切换的左边）：打开 SSH 默认两个都打开 -->
                            <el-radio-button value="sftp">SFTP</el-radio-button>
                            <el-radio-button value="info">信息</el-radio-button>
                            <el-radio-button value="processes">进程</el-radio-button>
                            <el-radio-button value="commands">命令</el-radio-button>
                            <el-radio-button value="network">网络</el-radio-button>
                            <el-radio-button value="tunnel">转发 / 代理</el-radio-button>
                            <el-radio-button value="ai">AI</el-radio-button>
                        </el-radio-group>
                        <el-icon class="side-close" @click="panelVisible = false">
                            <Close />
                        </el-icon>
                    </div>
                    <div class="side-body">
                        <SftpPanel v-show="panelTab === 'sftp'" :opts="activeTabOpts" :tab-key="store.activeKey"
                            :fav-key="activeTabConnId" />
                        <ServerInfoView v-show="panelTab === 'info'" :session-id="activeSessionId"
                            :active="infoActive" />
                        <ProcessManagerView v-show="panelTab === 'processes'" :session-id="activeSessionId"
                            :active="processActive" />
                        <CustomCommandsView v-show="panelTab === 'commands'" :session-id="activeSessionId" />
                        <NetworkView v-show="panelTab === 'network'" :session-id="activeSessionId"
                            :active="networkActive" />
                        <TunnelView v-show="panelTab === 'tunnel'" :session-id="activeSessionId"
                            :active="tunnelActive" />
                        <AiTerminalPanel v-show="panelTab === 'ai'" :session-id="activeSessionId" />
                    </div>
                </aside>
            </div>
        </template>

        <ContextMenu v-model="tabCtxVisible" :x="tabCtxX" :y="tabCtxY" :items="tabCtxItems" @pick="onTabCtxPick" />
        <ConnectDialog v-model="dialogVisible" mode="connect" conn-type="ssh" @connect="onConnect" />
    </div>
</template>

<script setup lang="ts">
import { computed, onActivated, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Monitor, Close, Plus, InfoFilled, CloseBold, DArrowRight, CircleClose } from '@element-plus/icons-vue'
import { on as busOn } from '../utils/bus'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import TerminalPane from '../components/TerminalPane.vue'
import SftpPanel from '../components/SftpPanel.vue'
import ServerInfoView from '../components/ServerInfoView.vue'
import ProcessManagerView from '../components/ProcessManagerView.vue'
import CustomCommandsView from '../components/CustomCommandsView.vue'
import NetworkView from '../components/NetworkView.vue'
import TunnelView from '../components/TunnelView.vue'
import AiTerminalPanel from '../components/AiTerminalPanel.vue'
import ConnectDialog from '../components/ConnectDialog.vue'
import { useTerminalStore, type TerminalTab } from '../stores/terminal'
import { useConnectionsStore } from '../stores/connections'
import { makeSavedConnection } from '../utils/wails'
import type { ConnectOptions } from '../utils/wails'

const store = useTerminalStore()
const connStore = useConnectionsStore()
const dialogVisible = ref(false)

// 标签页右键菜单
const tabCtxVisible = ref(false)
const tabCtxX = ref(0)
const tabCtxY = ref(0)
const tabCtxItems = ref<(CtxItem | 'divider')[]>([])
const tabCtxTab = ref<TerminalTab | null>(null)
const tabCtxIndex = ref(-1)

// 右侧信息面板：SFTP 文件为默认页（打开 SSH 终端即同时打开 SFTP）
const panelVisible = ref(true)
const panelTab = ref<'sftp' | 'info' | 'processes' | 'commands' | 'network' | 'tunnel' | 'ai'>('sftp')
const activeSessionId = computed(() => store.activeTab?.sessionId || '')
// SFTP 面板跟随当前活动标签的连接参数
const activeTabOpts = computed(() => store.activeTab?.opts ?? null)
// SFTP 面板的目录收藏按来源连接 id 区分（快速连接无 id → 不展示收藏入口）
const activeTabConnId = computed(() => store.activeTab?.connId ?? undefined)
// 服务器信息页激活时才会加载 / 刷新
const infoActive = computed(() => panelVisible.value && panelTab.value === 'info')
// 进程管理页激活时才会自动刷新
const processActive = computed(() => panelVisible.value && panelTab.value === 'processes')
// 网络页激活时才会自动刷新 / 实时采样
const networkActive = computed(() => panelVisible.value && panelTab.value === 'network')
// 转发 / 代理页激活时才会轮询隧道列表
const tunnelActive = computed(() => panelVisible.value && panelTab.value === 'tunnel')

// 从连接管理页「打开」SSH 进入时，确保右侧面板展开并显示 SFTP 页
onActivated(() => {
    const raw = sessionStorage.getItem('spark:open-terminal-panel')
    if (!raw) return
    sessionStorage.removeItem('spark:open-terminal-panel')
    panelVisible.value = true
    panelTab.value = 'sftp'
})

// 全局快捷键触发的动作（由 App.vue 通过事件总线转发）
let offNew: (() => void) | null = null
let offClose: (() => void) | null = null
let offPanel: (() => void) | null = null
let offShowSftp: (() => void) | null = null

onMounted(() => {
    offNew = busOn('terminal:new', () => {
        if (store.tabs.length === 0) return // 无标签时页面引导已可见
        openDialog()
    })
    offClose = busOn('terminal:close-tab', () => {
        if (store.activeKey) store.removeTab(store.activeKey)
    })
    offPanel = busOn('terminal:toggle-panel', () => {
        panelVisible.value = !panelVisible.value
    })
    // 快捷键 / 其他页面请求打开 SFTP 面板
    offShowSftp = busOn('terminal:show-sftp', () => {
        panelVisible.value = true
        panelTab.value = 'sftp'
    })
})

onBeforeUnmount(() => {
    offNew?.()
    offClose?.()
    offPanel?.()
    offShowSftp?.()
})

// 面板宽度：可拖拽调整，记住上次宽度
const panelWidth = ref(Number(localStorage.getItem('spark:panelWidth')) || 420)
const resizingPanel = ref(false)

function startResize(e: MouseEvent) {
    e.preventDefault()
    resizingPanel.value = true
    document.body.style.userSelect = 'none'
    const onMove = (ev: MouseEvent) => {
        const w = window.innerWidth - ev.clientX
        panelWidth.value = Math.min(640, Math.max(260, w))
    }
    const onUp = () => {
        resizingPanel.value = false
        document.body.style.userSelect = ''
        localStorage.setItem('spark:panelWidth', String(panelWidth.value))
        window.removeEventListener('mousemove', onMove)
        window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
}

function openDialog() {
    dialogVisible.value = true
}

function onMiddleClick(key: string, e: MouseEvent) {
    if (e.button === 1) store.removeTab(key)
}

// ---------- 标签页右键 ----------

function onTabContext(event: MouseEvent, tab: TerminalTab) {
    event.preventDefault()
    tabCtxTab.value = tab
    tabCtxIndex.value = store.tabs.findIndex((t) => t.key === tab.key)
    tabCtxItems.value = buildTabCtx()
    tabCtxX.value = event.clientX
    tabCtxY.value = event.clientY
    tabCtxVisible.value = false
    requestAnimationFrame(() => {
        tabCtxVisible.value = true
    })
}

function buildTabCtx(): (CtxItem | 'divider')[] {
    const total = store.tabs.length
    const idx = tabCtxIndex.value
    return [
        { key: 'close-current', label: '关闭当前', icon: Close, disabled: total === 0 },
        { key: 'close-others', label: '关闭其他', icon: CloseBold, disabled: total <= 1 },
        { key: 'close-right', label: '关闭右边', icon: DArrowRight, disabled: idx < 0 || idx >= total - 1 },
        { key: 'close-all', label: '关闭全部', icon: CircleClose, disabled: total === 0 },
    ]
}

function onTabCtxPick(item: CtxItem) {
    const tab = tabCtxTab.value
    if (!tab) return
    const idx = store.tabs.findIndex((t) => t.key === tab.key)
    switch (item.key) {
        case 'close-current':
            store.removeTab(tab.key)
            break
        case 'close-others':
            closeTabs(store.tabs.filter((t) => t.key !== tab.key))
            break
        case 'close-right':
            closeTabs(idx >= 0 ? store.tabs.slice(idx + 1) : [])
            break
        case 'close-all':
            closeTabs([...store.tabs])
            break
    }
}

function closeTabs(list: TerminalTab[]) {
    for (const t of list) store.removeTab(t.key)
}

async function onConnect(opts: ConnectOptions, save: boolean) {
    dialogVisible.value = false
    store.addTab(opts)
    // 打开 SSH 即默认同时打开 SFTP（右侧面板切到 SFTP 页并保持展开）
    panelVisible.value = true
    panelTab.value = 'sftp'
    if (save) {
        try {
            await connStore.create(
                makeSavedConnection({
                    name: `${opts.username}@${opts.host}:${opts.port || 22}`,
                    type: 'ssh',
                    host: opts.host,
                    port: opts.port,
                    username: opts.username,
                    password: opts.password,
                    useKey: opts.useKey,
                    privateKey: opts.privateKey,
                    passphrase: opts.passphrase,
                    forwardAgent: opts.forwardAgent,
                    defaultDir: opts.defaultDir || '',
                    tls: false,
                }),
            )
            ElMessage.success('已保存到连接列表')
        } catch (e: any) {
            ElMessage.error(`保存连接失败：${e?.message || e}`)
        }
    }
}
</script>

<style scoped>
.terminal-view {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.empty-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 16px;
    color: var(--text-secondary);
}

.tab-bar {
    display: flex;
    align-items: stretch;
    background: var(--tabbar-bg);
    border-bottom: 1px solid var(--border-color);
    overflow-x: auto;
    flex-shrink: 0;
}

.tab-container-container {
    display: flex;
    align-items: center;
    justify-content: space-between;
}

.tab {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    height: 38px;
    border-right: 1px solid var(--border-color);
    cursor: pointer;
    color: var(--text-secondary);
    font-size: 12.5px;
    white-space: nowrap;
    user-select: none;
    background: transparent;
}

.tab:hover {
    background: var(--tab-hover);
}

.tab.active {
    background: var(--panel-bg);
    color: var(--text-primary);
    box-shadow: inset 0 2px 0 var(--accent);
}

.tab-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
}

.tab-dot.connected {
    background: #34c759;
}

.tab-dot.connecting {
    background: #e6a23c;
    animation: pulse 1s infinite;
}

.tab-dot.closed,
.tab-dot.error {
    background: #f56c6c;
}

@keyframes pulse {
    50% {
        opacity: 0.35;
    }
}

.tab-close {
    border-radius: 4px;
    padding: 2px;
}

.tab-close:hover {
    background: var(--tab-hover-2);
    color: #fff;
}

.tab-add {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    cursor: pointer;
    color: var(--text-secondary);
}

.tab-add:hover {
    color: var(--text-primary);
    background: var(--tab-hover);
}

.tab-add.active {
    color: var(--accent);
}

.terminal-body {
    flex: 1;
    min-height: 0;
    display: flex;
}

.term-area {
    flex: 1;
    min-width: 0;
    position: relative;
}

.term-area>* {
    position: absolute;
    inset: 0;
}

.side-panel {
    flex-shrink: 0;
    border-left: 1px solid var(--border-color);
    background: var(--panel-bg);
    display: flex;
    flex-direction: column;
    min-height: 0;
    position: relative;
}

.side-panel.resizing {
    user-select: none;
}

.resize-handle {
    position: absolute;
    left: -5px;
    top: 0;
    bottom: 0;
    width: 10px;
    cursor: col-resize;
    z-index: 10;
}

.resize-handle::after {
    content: '';
    position: absolute;
    left: 3px;
    top: 0;
    bottom: 0;
    width: 2px;
    border-radius: 1px;
    background: transparent;
    transition: background 0.15s;
}

.resize-handle:hover::after,
.side-panel.resizing .resize-handle::after {
    background: var(--accent);
}

.side-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    padding: 6px 8px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.side-head :deep(.el-radio-group) {
    flex: 1;
    min-width: 0;
    overflow-x: auto;
    white-space: nowrap;
}

.side-close {
    cursor: pointer;
    color: var(--text-secondary);
    padding: 4px;
    border-radius: 4px;
}

.side-close:hover {
    color: var(--text-primary);
    background: var(--tab-hover-2);
}

.side-body {
    flex: 1;
    min-height: 0;
}

.side-body>* {
    height: 100%;
}
</style>
