<template>
  <div class="local-term-view">
    <!-- 安卓端不支持本地终端：屏蔽显示 -->
    <div v-if="android" class="empty-state">
      <el-icon :size="44" color="var(--border-strong)">
        <Platform />
      </el-icon>
      <p>本地终端在安卓端不可用</p>
      <p class="sub">安卓系统不允许应用直接启动本机 shell</p>
    </div>

    <template v-else>
      <div v-if="store.tabs.length === 0" class="empty-state">
        <el-icon :size="44" color="var(--border-strong)">
          <Platform />
        </el-icon>
        <p>还没有本地终端</p>
        <div class="empty-actions">
          <el-select v-model="shellChoice" size="default" style="width: 180px">
            <el-option label="默认（cmd.exe）" value="" />
            <el-option label="PowerShell" value="powershell" />
          </el-select>
          <el-button type="primary" @click="newTerminal">新建本地终端</el-button>
        </div>
        <p class="sub">在本机打开 shell，可同时打开多个；PowerShell 支持 ls 等 Unix 习惯命令</p>
      </div>

      <template v-else>
        <div class="tab-bar">
          <div v-for="tab in store.tabs" :key="tab.key" class="tab"
            :class="{ active: tab.key === store.activeKey }" @click="store.setActive(tab.key)"
            @auxclick="onMiddleClick(tab.key, $event)" @contextmenu.prevent="onTabContext($event, tab)">
            <span class="tab-dot" :class="tab.status"></span>
            <span class="tab-title">{{ tab.title }}<template v-if="tab.shell">（{{ shellLabel(tab.shell) }}）</template></span>
            <el-icon class="tab-close" @click.stop="store.removeTab(tab.key)">
              <Close />
            </el-icon>
          </div>
          <el-select v-model="shellChoice" size="small" class="tab-shell-select"
            title="新终端的 shell">
            <el-option label="cmd.exe" value="" />
            <el-option label="PowerShell" value="powershell" />
          </el-select>
          <div class="tab-add" title="新建本地终端" @click="newTerminal">
            <el-icon>
              <Plus />
            </el-icon>
          </div>
        </div>
        <div class="term-area">
          <LocalTerminalPane v-for="tab in store.tabs" v-show="tab.key === store.activeKey" :key="tab.key"
            :tab="tab" />
        </div>
      </template>
    </template>

    <ContextMenu v-model="tabCtxVisible" :x="tabCtxX" :y="tabCtxY" :items="tabCtxItems" @pick="onTabCtxPick" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { Platform, Close, Plus, CloseBold, DArrowRight, CircleClose } from '@element-plus/icons-vue'
import LocalTerminalPane from '../components/LocalTerminalPane.vue'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import { useLocalTerminalStore, type LocalTerminalTab } from '../stores/localTerminal'
import { isAndroidApp } from '../utils/platform'

const store = useLocalTerminalStore()
const android = isAndroidApp()

// 标签页右键菜单
const tabCtxVisible = ref(false)
const tabCtxX = ref(0)
const tabCtxY = ref(0)
const tabCtxItems = ref<(CtxItem | 'divider')[]>([])
const tabCtxTab = ref<LocalTerminalTab | null>(null)
const tabCtxIndex = ref(-1)

// 新建终端使用的 shell（'' = 平台默认，Windows 为 cmd.exe）
const shellChoice = ref('')

function shellLabel(shell: string): string {
  return shell === 'powershell' ? 'PowerShell' : shell || 'cmd'
}

function newTerminal() {
  store.addTab(shellChoice.value)
}

function onMiddleClick(key: string, e: MouseEvent) {
  if (e.button === 1) store.removeTab(key)
}

// ---------- 标签页右键 ----------

function onTabContext(event: MouseEvent, tab: LocalTerminalTab) {
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

function closeTabs(list: LocalTerminalTab[]) {
  for (const t of list) store.removeTab(t.key)
}
</script>

<style scoped>
.local-term-view {
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
  gap: 14px;
  color: var(--text-secondary);
}

.empty-state p {
  margin: 0;
  font-size: 13px;
}

.empty-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.tab-shell-select {
  width: 110px;
  margin-left: auto;
  flex-shrink: 0;
  align-self: center;
}

.empty-state .sub {
  font-size: 12px;
  opacity: 0.75;
  max-width: 480px;
  text-align: center;
}

.tab-bar {
  display: flex;
  align-items: stretch;
  background: var(--tabbar-bg);
  border-bottom: 1px solid var(--border-color);
  overflow-x: auto;
  flex-shrink: 0;
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

.tab-dot.running {
  background: #34c759;
}

.tab-dot.starting {
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

.term-area {
  flex: 1;
  min-height: 0;
  position: relative;
}

.term-area > * {
  position: absolute;
  inset: 0;
}
</style>
