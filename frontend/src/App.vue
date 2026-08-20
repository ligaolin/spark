<template>
  <el-config-provider :locale="zhCn">
    <div class="app-shell">
      <aside class="sidebar">
        <div class="brand">
          <span class="brand-logo">⚡</span>
          <span class="brand-name">Spark</span>
          <span class="brand-sub">终端工具</span>
        </div>
        <nav class="nav">
          <router-link
            v-for="item in menu"
            :key="item.path"
            :to="item.path"
            class="nav-item"
            :class="{ active: route.path === item.path }"
          >
            <el-icon class="nav-icon"><component :is="item.icon" /></el-icon>
            <span>{{ item.label }}</span>
          </router-link>
        </nav>
        <div class="sidebar-foot">
          <router-link to="/settings" class="foot-link" :class="{ active: route.path === '/settings' }">
            <el-icon class="nav-icon"><Setting /></el-icon>
            <span>设置</span>
          </router-link>
        </div>
      </aside>
      <main class="content">
        <router-view v-slot="{ Component }">
          <keep-alive>
            <component :is="Component" />
          </keep-alive>
        </router-view>
      </main>
    </div>
    <!-- 页面内弹窗宿主（InputDialog / ConfirmDialog） -->
    <DialogHost />
  </el-config-provider>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import { useRoute, useRouter } from 'vue-router'
import { Window } from '@wailsio/runtime'
import { Monitor, FolderOpened, Connection, Link, Setting, Notebook, Collection } from '@element-plus/icons-vue'
import DialogHost from './components/DialogHost.vue'
import { useShortcutsStore, eventToCombo } from './stores/shortcuts'
import { emit } from './utils/bus'

const route = useRoute()
const router = useRouter()
const shortcuts = useShortcutsStore()

const menu = [
  { path: '/connections', label: '连接管理', icon: Connection },
  { path: '/terminal', label: 'SSH 终端', icon: Monitor },
  { path: '/sftp', label: 'SFTP 文件', icon: FolderOpened },
  { path: '/ftp', label: 'FTP 文件', icon: Link },
  { path: '/documents', label: '文档管理', icon: Notebook },
  { path: '/sites', label: '站点管理', icon: Collection },
]

// 全局快捷键分发（输入框/终端等可输入区域不拦截；纯功能键 F1~F12 除外）
function onKeyDown(e: KeyboardEvent) {
  const t = e.target as HTMLElement | null
  const combo = eventToCombo(e)
  const isPureFunctionKey = !!combo && /^f(\d{1,2})$/.test(combo)
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable) && !isPureFunctionKey) {
    return
  }
  if (!combo) return
  const action = shortcuts.items.find((a) => a.key.toLowerCase() === combo)
  if (!action) return
  e.preventDefault()
  switch (action.id) {
    case 'nav.connections':
      router.push('/connections')
      break
    case 'nav.terminal':
      router.push('/terminal')
      break
    case 'nav.sftp':
      router.push('/sftp')
      break
    case 'nav.ftp':
      router.push('/ftp')
      break
    case 'nav.documents':
      router.push('/documents')
      break
    case 'nav.sites':
      router.push('/sites')
      break
    case 'terminal.new':
      router.push('/terminal')
      emit('terminal:new')
      break
    case 'terminal.close':
      emit('terminal:close-tab')
      break
    case 'panel.toggle':
      emit('terminal:toggle-panel')
      break
    case 'devtools.toggle':
      Window.OpenDevTools()
      break
  }
}

onMounted(() => {
  shortcuts.load()
  window.addEventListener('keydown', onKeyDown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
})
</script>

<style scoped>
.app-shell {
  display: flex;
  height: 100vh;
}

.sidebar {
  width: 190px;
  flex-shrink: 0;
  background: #131519;
  border-right: 1px solid var(--border-color);
  display: flex;
  flex-direction: column;
}

.brand {
  padding: 18px 16px 14px;
  display: flex;
  align-items: baseline;
  gap: 8px;
  border-bottom: 1px solid var(--border-color);
}

.brand-logo {
  font-size: 18px;
}

.brand-name {
  font-size: 17px;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.brand-sub {
  font-size: 11px;
  color: var(--text-secondary);
}

.nav {
  flex: 1;
  padding: 10px 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border-radius: 6px;
  color: var(--text-secondary);
  text-decoration: none;
  font-size: 13px;
  transition: background 0.15s, color 0.15s;
}

.nav-item:hover {
  background: #1e222b;
  color: var(--text-primary);
}

.nav-item.active {
  background: #233049;
  color: #7fb0ff;
}

.nav-icon {
  font-size: 15px;
}

.sidebar-footer {
  padding: 12px 16px;
  font-size: 11px;
  color: #4a5060;
  border-top: 1px solid var(--border-color);
}

.sidebar-foot {
  padding: 8px;
  border-top: 1px solid var(--border-color);
}

.foot-link {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border-radius: 6px;
  color: var(--text-secondary);
  text-decoration: none;
  font-size: 13px;
  transition: background 0.15s, color 0.15s;
}

.foot-link:hover {
  background: #1e222b;
  color: var(--text-primary);
}

.foot-link.active {
  background: #233049;
  color: #7fb0ff;
}

.content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
</style>
