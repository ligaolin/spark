<template>
    <el-config-provider :locale="zhCn">
        <div class="app-shell">
            <aside class="sidebar">
                <div class="brand">
                    <span class="brand-logo">⚡</span>
                    <span class="brand-name">Spark</span>
                    <span class="brand-sub">终端</span>
                </div>
                <nav class="nav">
                    <router-link v-for="item in menu" :key="item.path" :to="item.path" class="nav-item"
                        :class="{ active: route.path === item.path }">
                        <el-icon class="nav-icon">
                            <component :is="item.icon" />
                        </el-icon>
                        <span>{{ item.label }}</span>
                    </router-link>
                </nav>
                <div class="sidebar-foot">
                    <div class="foot-row">
                        <router-link to="/settings" class="foot-link" :class="{ active: route.path === '/settings' }">
                            <el-icon class="nav-icon">
                                <Setting />
                            </el-icon>
                            <span>设置</span>
                        </router-link>
                        <button class="theme-toggle" :title="isDark ? '切换到亮色主题' : '切换到暗色主题'" @click="toggleTheme">
                            <el-icon class="nav-icon">
                                <component :is="isDark ? Sunny : Moon" />
                            </el-icon>
                        </button>
                    </div>
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
        <!-- 新版本检查 / 下载弹窗 -->
        <UpdateDialog />
    </el-config-provider>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import { useRoute, useRouter } from 'vue-router'
import { Window } from '@wailsio/runtime'
import {
    Monitor,
    Connection,
    Link,
    Setting,
    Notebook,
    Collection,
    EditPen,
    Platform,
    Sunny,
    Moon,
} from '@element-plus/icons-vue'
import DialogHost from './components/DialogHost.vue'
import UpdateDialog from './components/UpdateDialog.vue'
import { useShortcutsStore, eventToCombo } from './stores/shortcuts'
import { useSettingsStore } from './stores/settings'
import { applyTheme, cacheTheme } from './utils/theme'
import { emit } from './utils/bus'
import { isAndroidApp } from './utils/platform'
import { checkForUpdates } from './utils/updateCheck'

const route = useRoute()
const router = useRouter()
const shortcuts = useShortcutsStore()
const settings = useSettingsStore()

const isDark = computed(() => settings.theme === 'dark')

async function toggleTheme() {
    try {
        await settings.setTheme(isDark.value ? 'light' : 'dark')
    } catch {
        // 持久化失败时也本地生效（不阻断切换）
        const next: 'dark' | 'light' = isDark.value ? 'light' : 'dark'
        applyTheme(next)
        cacheTheme(next)
    }
}

const menu = [
    ...(isAndroidApp() ? [] : [{ path: '/local-terminal', label: '本地终端', icon: Platform }]),
    { path: '/connections', label: '连接管理', icon: Connection },
    { path: '/terminal', label: 'SSH 终端', icon: Monitor },
    // SFTP 已并入 SSH 终端右侧面板（SFTP 文件页），不再单独列在侧边栏
    { path: '/ftp', label: 'FTP 文件', icon: Link },

    { path: '/documents', label: '文档管理', icon: Notebook },
    { path: '/sites', label: '站点管理', icon: Collection },
    { path: '/remote-editor', label: '编辑器', icon: EditPen },
    // 本地终端：安卓端无法启动本机 shell，直接屏蔽显示
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
            router.push('/terminal')
            emit('terminal:show-sftp')
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
        case 'nav.local-terminal':
            router.push('/local-terminal')
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
    settings.load().then(() => {
        // 数据库设置为准，覆盖启动时的本地缓存
        applyTheme(settings.theme)
        cacheTheme(settings.theme)
    })
    window.addEventListener('keydown', onKeyDown)
    // 启动后延迟检查 GitHub 新版本：有新版时弹窗提示可点击下载更新。
    // 桌面端自动替换二进制并重启；安卓端下载 APK 后调起系统安装器。
    setTimeout(() => {
        void checkForUpdates(true)
    }, 1500)
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
    background: var(--sidebar-bg);
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
    background: var(--hover-bg);
    color: var(--text-primary);
}

.nav-item.active {
    background: var(--active-bg);
    color: var(--active-text);
}

.nav-icon {
    font-size: 15px;
}

.sidebar-footer {
    padding: 12px 16px;
    font-size: 11px;
    color: var(--text-muted);
    border-top: 1px solid var(--border-color);
}

.sidebar-foot {
    padding: 8px;
    border-top: 1px solid var(--border-color);
}

.foot-row {
    display: flex;
    align-items: center;
    gap: 4px;
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
    flex: 1;
    transition: background 0.15s, color 0.15s;
}

.foot-link:hover {
    background: var(--hover-bg);
    color: var(--text-primary);
}

.foot-link.active {
    background: var(--active-bg);
    color: var(--active-text);
}

/* 主题切换按钮：设置入口右侧的图标按钮 */
.theme-toggle {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    flex-shrink: 0;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
}

.theme-toggle:hover {
    background: var(--hover-bg);
    color: var(--text-primary);
}

.content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
}
</style>
