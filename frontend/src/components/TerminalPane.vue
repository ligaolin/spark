<template>
    <div class="terminal-pane" @contextmenu.prevent="onContextMenu" @keydown="onTermKeydown">
        <div class="term-main">
            <div ref="termRef" class="term-container" />
            <div v-if="tab.status === 'connecting'" class="term-overlay">
                <el-icon class="is-loading" :size="28">
                    <Loading />
                </el-icon>
                <span>正在连接 {{ tab.opts.username }}@{{ tab.opts.host }}:{{ tab.opts.port || 22 }} ...</span>
            </div>
            <div v-else-if="tab.status === 'error'" class="term-overlay">
                <el-icon :size="28" color="#f56c6c">
                    <CircleCloseFilled />
                </el-icon>
                <span>连接失败：{{ tab.error }}</span>
                <el-button size="small" type="primary" @click="connect">重试</el-button>
                <el-button size="small" @click="closeTab">关闭</el-button>
            </div>
            <div v-else-if="tab.status === 'closed'" class="term-overlay">
                <el-icon :size="28" color="#e6a23c">
                    <WarningFilled />
                </el-icon>
                <span>会话已结束（退出码 {{ tab.exitCode }}）</span>
                <span v-if="tab.error" class="term-err">{{ tab.error }}</span>
                <el-button size="small" type="primary" @click="reconnect">重新连接</el-button>
                <el-button size="small" @click="closeTab">关闭</el-button>
            </div>
        </div>

        <div class="term-statusbar">
            <template v-if="allIps.length">
                <div v-for="ip in allIps" :key="ip">
                    <span class="sb-ip mono" :title="ip">{{ ip }}</span>
                    <el-icon class="sb-copy" title="复制" @click="copyIp(ip)" size="14">
                        <CopyDocument />
                    </el-icon>
                </div>
            </template>
            <span v-else class="sb-ip dim">—</span>
        </div>

        <ContextMenu v-model="ctxVisible" :x="ctxX" :y="ctxY" :items="ctxItems" @pick="onCtxPick" />
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, nextTick } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from '@xterm/addon-fit'
import 'xterm/css/xterm.css'
import { Events, Clipboard } from '@wailsio/runtime'
import { ElMessage } from 'element-plus'
import { EVENTS, TerminalService } from '../utils/wails'
import { useTerminalStore, type TerminalTab } from '../stores/terminal'
import { useSettingsStore } from '../stores/settings'
import { resolveHostKeyIssue } from '../utils/hostkey'
import ContextMenu from './ContextMenu.vue'
import type { CtxItem } from './ContextMenu.vue'
import { eventToCombo } from '../stores/shortcuts'
import {
    Loading,
    CircleCloseFilled,
    WarningFilled,
    CopyDocument,
    Document,
    Select,
    Delete,
} from '@element-plus/icons-vue'

const props = defineProps<{ tab: TerminalTab }>()

const store = useTerminalStore()
const settings = useSettingsStore()
const termRef = ref<HTMLElement>()
const sessionIdRef = ref('')

// 底部状态栏 IP：连接地址 + 服务器网卡上的全部 IPv4（去重合并）
const backendIps = ref<string[]>([])
const allIps = computed(() => {
    const host = props.tab.opts?.host?.trim() ?? ''
    const ips = backendIps.value.filter((ip) => ip !== host)
    return host ? [host, ...ips] : ips
})

// 终端右键菜单状态
const ctxVisible = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)
const ctxItems = ref<(CtxItem | 'divider')[]>([])

function hasSelection(): boolean {
    return !!term && term.getSelection().length > 0
}

function onContextMenu(e: MouseEvent) {
    ctxX.value = e.clientX
    ctxY.value = e.clientY
    ctxItems.value = [
        { key: 'copy', label: '复制', icon: CopyDocument, hint: 'Ctrl+Shift+C', disabled: !hasSelection() },
        { key: 'paste', label: '粘贴', icon: Document, hint: 'Ctrl+Shift+V' },
        { key: 'select-all', label: '全选', icon: Select },
        'divider',
        { key: 'clear', label: '清屏', icon: Delete },
    ]
    ctxVisible.value = false
    requestAnimationFrame(() => {
        ctxVisible.value = true
    })
}

// 终端键盘快捷键：Ctrl+Shift+C / Ctrl+Insert 复制，Ctrl+Shift+V / Shift+Insert 粘贴。
// 注意：Ctrl+C 保留为中断信号（不拦截），Ctrl+V 走浏览器原生粘贴。
function onTermKeydown(e: KeyboardEvent) {
    const combo = eventToCombo(e)
    if (combo === 'ctrl+shift+c' || combo === 'ctrl+insert') {
        e.preventDefault()
        doCopy()
    } else if (combo === 'ctrl+shift+v' || combo === 'shift+insert') {
        e.preventDefault()
        doPaste()
    }
}

async function doCopy() {
    if (!term) return
    const sel = term.getSelection()
    if (!sel) return
    try {
        await Clipboard.SetText(sel)
    } catch (err: any) {
        ElMessage.error(`复制失败：${err?.message || err}`)
    }
}

async function doPaste() {
    if (!term) return
    try {
        const text = await Clipboard.Text()
        if (text) {
            term.paste(text)
        }
    } catch (err: any) {
        ElMessage.error(`读取剪贴板失败：${err?.message || err}`)
    }
}

async function onCtxPick(item: CtxItem) {
    if (!term) return
    switch (item.key) {
        case 'copy': {
            const sel = term.getSelection()
            if (!sel) return
            try {
                await Clipboard.SetText(sel)
            } catch (e: any) {
                ElMessage.error(`复制失败：${e?.message || e}`)
            }
            break
        }
        case 'paste': {
            try {
                const text = await Clipboard.Text()
                if (text) {
                    term.paste(text)
                }
            } catch (e: any) {
                ElMessage.error(`读取剪贴板失败：${e?.message || e}`)
            }
            break
        }
        case 'select-all':
            term.selectAll()
            break
        case 'clear':
            term.clear()
            break
    }
}

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let observer: ResizeObserver | null = null
let unOutput: (() => void) | null = null
let unExit: (() => void) | null = null

async function connect(hostKeyRetry = 0) {
    if (!term || !fitAddon) return
    sessionIdRef.value = ''
    backendIps.value = []
    store.markConnecting(props.tab.key)
    term.clear()
    fitAddon.fit()
    const opts = {
        ...props.tab.opts,
        rows: term.rows,
        cols: term.cols,
    }
    try {
        const id = await TerminalService.Connect(opts)
        sessionIdRef.value = id
        store.markConnected(props.tab.key, id)
        void loadIpStatus()
    } catch (e: any) {
        // 主机密钥未信任 / 不匹配：询问用户后保存密钥并重试一次
        if (hostKeyRetry === 0) {
            const accepted = await resolveHostKeyIssue(e, opts)
            if (accepted) {
                await connect(1)
                return
            }
        }
        store.markError(props.tab.key, e?.message || String(e))
    }
}

async function reconnect() {
    await connect()
}

function closeTab() {
    store.removeTab(props.tab.key)
}

// 拉取公网 / 内网 IP（用于底部状态栏）
async function loadIpStatus() {
    const id = sessionIdRef.value
    if (!id) return
    try {
        const st = await TerminalService.IpStatus(id)
        backendIps.value = st?.ips || []
    } catch {
        // 静默：状态栏仍展示连接地址
    }
}

async function copyIp(ip: string) {
    if (!ip) return
    try {
        await Clipboard.SetText(ip)
        ElMessage.success(`已复制 ${ip}`)
    } catch (e: any) {
        ElMessage.error(`复制失败：${e?.message || e}`)
    }
}

// xterm 主题色：随明暗主题切换
function xtermTheme(): Record<string, string> {
    return settings.theme === 'dark'
        ? {
            background: '#0f1115',
            foreground: '#d6d9e0',
            cursor: '#4f8cff',
            selectionBackground: '#2b3a55',
        }
        : {
            background: '#f8fafc',
            foreground: '#2b3040',
            cursor: '#3b82f6',
            selectionBackground: '#cfe0ff',
        }
}

onMounted(async () => {
    term = new Terminal({
        cursorBlink: true,
        fontSize: settings.terminalFontSize,
        fontFamily: 'Cascadia Code, JetBrains Mono, Consolas, monospace',
        theme: xtermTheme(),
        scrollback: 8000,
    })
    fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(termRef.value!)
    fitAddon.fit()

    // 用户输入 -> 后端
    term.onData((data) => {
        const id = sessionIdRef.value
        if (id) {
            TerminalService.Write(id, data).catch(() => undefined)
        }
    })

    // 输出事件 -> xterm
    unOutput = Events.On(EVENTS.terminalOutput, (evt: any) => {
        const out = evt.data
        if (out && out.sessionId === sessionIdRef.value && term) {
            term.write(out.data)
        }
    })

    // 会话结束事件
    unExit = Events.On(EVENTS.terminalExit, (evt: any) => {
        const ex = evt.data
        if (ex && ex.sessionId === sessionIdRef.value) {
            store.markClosed(props.tab.key, ex.code, ex.error)
            sessionIdRef.value = ''
        }
    })

    // 容器尺寸变化 -> fit + 通知后端 resize
    observer = new ResizeObserver(() => {
        if (!term || !fitAddon) return
        const oldRows = term.rows
        const oldCols = term.cols
        try {
            fitAddon.fit()
        } catch {
            return
        }
        if (term.rows !== oldRows || term.cols !== oldCols) {
            const id = sessionIdRef.value
            if (id) {
                TerminalService.Resize(id, term.rows, term.cols).catch(() => undefined)
            }
        }
    })
    observer.observe(termRef.value!)

    if (props.tab.status === 'connecting') {
        await connect()
    } else if (props.tab.sessionId) {
        sessionIdRef.value = props.tab.sessionId
        await loadIpStatus()
    }
})

// 设置里调整终端字号后即时生效
watch(
    () => settings.terminalFontSize,
    (size) => {
        if (term) {
            term.options.fontSize = size
            fitAddon?.fit()
            const id = sessionIdRef.value
            if (id) {
                TerminalService.Resize(id, term.rows, term.cols).catch(() => undefined)
            }
        }
    },
)

// 明暗主题切换后即时刷新 xterm 配色
watch(
    () => settings.theme,
    () => {
        if (term) {
            term.options.theme = xtermTheme()
            term.refresh(0, term.rows - 1)
        }
    },
)

onBeforeUnmount(() => {
    observer?.disconnect()
    unOutput?.()
    unExit?.()
    term?.dispose()
    term = null
})
</script>

<style scoped>
.terminal-pane {
    position: relative;
    width: 100%;
    height: 100%;
    background: var(--term-bg);
    display: flex;
    flex-direction: column;
}

.term-main {
    flex: 1;
    min-height: 0;
    position: relative;
}

.term-container {
    width: 100%;
    height: 100%;
    padding: 6px 0 6px 8px;
}

.term-container :deep(.xterm) {
    height: 100%;
}

.term-statusbar {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 6px 10px 8px;
    border-top: 1px solid var(--border-color);
    background: var(--panel-bg);
    font-size: 12px;
    color: var(--text-secondary);
    overflow-x: auto;
    white-space: nowrap;
}

.term-statusbar>div {
    display: flex;
    align-items: center;
    gap: 4px;
}

.sb-ip {
    color: var(--text-primary);
}

.sb-ip.dim {
    color: var(--text-muted);
}

.sb-copy {
    cursor: pointer;
    color: var(--text-secondary);
    border-radius: 3px;
    padding: 1px;
    flex-shrink: 0;
}

.sb-copy:hover {
    color: var(--active-text);
    background: var(--tab-hover-2);
}

.term-overlay {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 14px;
    background: var(--overlay-bg);
    color: var(--text-secondary);
    font-size: 13px;
    z-index: 10;
}

.term-err {
    max-width: 80%;
    font-size: 12px;
    color: #f56c6c;
    text-align: center;
    word-break: break-all;
}
</style>
