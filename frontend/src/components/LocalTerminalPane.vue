<template>
  <div class="local-term-pane" @contextmenu.prevent="onContextMenu" @keydown="onTermKeydown">
    <div ref="termRef" class="term-container" />
    <div v-if="tab.status === 'starting'" class="term-overlay">
      <el-icon class="is-loading" :size="28"><Loading /></el-icon>
      <span>正在启动本地终端…</span>
    </div>
    <div v-else-if="tab.status === 'error'" class="term-overlay">
      <el-icon :size="28" color="#f56c6c"><CircleCloseFilled /></el-icon>
      <span>启动失败：{{ tab.error }}</span>
      <el-button size="small" type="primary" @click="create">重试</el-button>
      <el-button size="small" @click="closeTab">关闭</el-button>
    </div>
    <div v-else-if="tab.status === 'closed'" class="term-overlay">
      <el-icon :size="28" color="#e6a23c"><WarningFilled /></el-icon>
      <span>本地终端已退出（退出码 {{ tab.exitCode }}）</span>
      <span v-if="tab.error" class="term-err">{{ tab.error }}</span>
      <el-button size="small" type="primary" @click="create">重新打开</el-button>
      <el-button size="small" @click="closeTab">关闭</el-button>
    </div>

    <ContextMenu v-model="ctxVisible" :x="ctxX" :y="ctxY" :items="ctxItems" @pick="onCtxPick" />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from '@xterm/addon-fit'
import 'xterm/css/xterm.css'
import { Events, Clipboard } from '@wailsio/runtime'
import { ElMessage } from 'element-plus'
import { EVENTS, LocalTerminalService } from '../utils/wails'
import { useLocalTerminalStore, type LocalTerminalTab } from '../stores/localTerminal'
import { useSettingsStore } from '../stores/settings'
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

const props = defineProps<{ tab: LocalTerminalTab }>()

const store = useLocalTerminalStore()
const settings = useSettingsStore()
const termRef = ref<HTMLElement>()

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
    if (text) term.paste(text)
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
        if (text) term.paste(text)
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
let createSeq = 0

async function create() {
  if (!term || !fitAddon) return
  const seq = ++createSeq
  store.markStarting(props.tab.key)
  term.clear()
  fitAddon.fit()
  try {
    const id = await LocalTerminalService.Create(props.tab.shell, props.tab.cwd || '', term.rows, term.cols)
    if (seq !== createSeq) return // 已重连/关闭
    store.markRunning(props.tab.key, id)
  } catch (e: any) {
    if (seq !== createSeq) return
    store.markError(props.tab.key, e?.message || String(e))
  }
}

function closeTab() {
  store.removeTab(props.tab.key)
}

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

  term.onData((data) => {
    const id = props.tab.sessionId
    if (id) {
      LocalTerminalService.Write(id, data).catch(() => undefined)
    }
  })

  unOutput = Events.On(EVENTS.localTerminalOutput, (evt: any) => {
    const out = evt.data
    if (out && out.sessionId === props.tab.sessionId && term) {
      term.write(out.data)
    }
  })

  unExit = Events.On(EVENTS.localTerminalExit, (evt: any) => {
    const ex = evt.data
    if (ex && ex.sessionId === props.tab.sessionId) {
      store.markClosed(props.tab.key, ex.code, ex.error)
    }
  })

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
      const id = props.tab.sessionId
      if (id) {
        LocalTerminalService.Resize(id, term.rows, term.cols).catch(() => undefined)
      }
    }
  })
  observer.observe(termRef.value!)

  if (props.tab.status === 'starting') {
    await create()
  }
})

watch(
  () => settings.terminalFontSize,
  (size) => {
    if (term) {
      term.options.fontSize = size
      fitAddon?.fit()
    }
  },
)

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
  createSeq++
  observer?.disconnect()
  unOutput?.()
  unExit?.()
  term?.dispose()
  term = null
})
</script>

<style scoped>
.local-term-pane {
  position: relative;
  width: 100%;
  height: 100%;
  background: var(--term-bg);
}

.term-container {
  width: 100%;
  height: 100%;
  padding: 6px 0 6px 8px;
}

.term-container :deep(.xterm) {
  height: 100%;
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
