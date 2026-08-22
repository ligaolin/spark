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
            @auxclick="onMiddleClick(tab.key, $event)">
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
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { Platform, Close, Plus } from '@element-plus/icons-vue'
import LocalTerminalPane from '../components/LocalTerminalPane.vue'
import { useLocalTerminalStore } from '../stores/localTerminal'
import { isAndroidApp } from '../utils/platform'

const store = useLocalTerminalStore()
const android = isAndroidApp()

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
