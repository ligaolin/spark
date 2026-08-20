<template>
    <div class="terminal-view">
        <div v-if="store.tabs.length === 0" class="empty-state">
            <el-icon :size="44" color="#3a4152">
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
                        @auxclick="onMiddleClick(tab.key, $event)">
                        <span class="tab-dot" :class="tab.status"></span>
                        <span class="tab-title">{{ tab.title }}</span>
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
                    :title="panelVisible ? '收起信息面板' : '展开信息面板（服务器信息 / 自定义命令）'" @click="panelVisible = !panelVisible">
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

                <aside v-if="panelVisible" class="side-panel" :class="{ resizing: resizingPanel }"
                    :style="{ width: panelWidth + 'px' }">
                    <div class="resize-handle" title="拖拽调整宽度" @mousedown="startResize" />
                    <div class="side-head">
                        <el-radio-group v-model="panelTab" size="small">
                            <el-radio-button value="info">服务器信息</el-radio-button>
                            <el-radio-button value="processes">进程管理</el-radio-button>
                            <el-radio-button value="commands">自定义命令</el-radio-button>
                        </el-radio-group>
                        <el-icon class="side-close" @click="panelVisible = false">
                            <Close />
                        </el-icon>
                    </div>
                    <div class="side-body">
                        <ServerInfoView v-show="panelTab === 'info'" :session-id="activeSessionId" />
                        <ProcessManagerView v-show="panelTab === 'processes'" :session-id="activeSessionId"
                            :active="processActive" />
                        <CustomCommandsView v-show="panelTab === 'commands'" :session-id="activeSessionId" />
                    </div>
                </aside>
            </div>
        </template>

        <ConnectDialog v-model="dialogVisible" mode="connect" conn-type="ssh" @connect="onConnect" />
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Monitor, Close, Plus, InfoFilled } from '@element-plus/icons-vue'
import { on as busOn } from '../utils/bus'
import TerminalPane from '../components/TerminalPane.vue'
import ServerInfoView from '../components/ServerInfoView.vue'
import ProcessManagerView from '../components/ProcessManagerView.vue'
import CustomCommandsView from '../components/CustomCommandsView.vue'
import ConnectDialog from '../components/ConnectDialog.vue'
import { useTerminalStore } from '../stores/terminal'
import { useConnectionsStore } from '../stores/connections'
import { makeSavedConnection } from '../utils/wails'
import type { ConnectOptions } from '../utils/wails'

const store = useTerminalStore()
const connStore = useConnectionsStore()
const dialogVisible = ref(false)

// 右侧信息面板
const panelVisible = ref(true)
const panelTab = ref<'info' | 'processes' | 'commands'>('info')
const activeSessionId = computed(() => store.activeTab?.sessionId || '')
// 进程管理页激活时才会自动刷新
const processActive = computed(() => panelVisible.value && panelTab.value === 'processes')

// 全局快捷键触发的动作（由 App.vue 通过事件总线转发）
let offNew: (() => void) | null = null
let offClose: (() => void) | null = null
let offPanel: (() => void) | null = null

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
})

onBeforeUnmount(() => {
    offNew?.()
    offClose?.()
    offPanel?.()
})

// 面板宽度：可拖拽调整，记住上次宽度
const panelWidth = ref(Number(localStorage.getItem('spark:panelWidth')) || 340)
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

async function onConnect(opts: ConnectOptions, save: boolean) {
    dialogVisible.value = false
    store.addTab(opts)
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
    background: #14161b;
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
    background: #1b1e26;
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
    background: #2c303b;
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
    background: #1b1e26;
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
    padding: 6px 8px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.side-close {
    cursor: pointer;
    color: var(--text-secondary);
    padding: 4px;
    border-radius: 4px;
}

.side-close:hover {
    color: var(--text-primary);
    background: #2c303b;
}

.side-body {
    flex: 1;
    min-height: 0;
}

.side-body>* {
    height: 100%;
}
</style>
