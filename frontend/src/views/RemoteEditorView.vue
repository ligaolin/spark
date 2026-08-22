<template>
  <div class="remote-editor-view">
    <!-- 顶部：编辑器板块标签（动态，可开多个、可关闭） -->
    <div class="re-toolbar">
      <el-button size="small" type="primary" plain @click="openLocalFolder">
        <el-icon><FolderOpened /></el-icon><span>打开本地文件夹</span>
      </el-button>
      <el-button size="small" :disabled="!remoteEditor.panels.length" @click="closeAll">
        关闭全部
      </el-button>
      <span v-if="remoteEditor.panels.length" class="re-hint">板块互相独立，关闭后释放编辑器资源</span>
    </div>

    <div v-if="remoteEditor.panels.length" class="re-body">
      <div class="re-tabs">
        <div
          v-for="p in remoteEditor.panels"
          :key="p.id"
          class="re-tab"
          :class="{ active: remoteEditor.activeId === p.id }"
          @click="remoteEditor.activeId = p.id"
          @contextmenu.prevent="onTabContext($event, p)"
        >
          <el-icon class="re-tab-icon"><EditPen /></el-icon>
          <span class="re-tab-title" :title="p.rootPath">{{ p.title }}</span>
          <el-icon class="re-tab-close" title="关闭板块（释放编辑器资源）" @click.stop="closePanel(p.id)">
            <Close />
          </el-icon>
        </div>
      </div>
      <div class="re-panels">
        <RemoteEditorPanel
          v-for="p in remoteEditor.panels"
          v-show="remoteEditor.activeId === p.id"
          :key="p.id"
          :ref="(el: any) => setPanelRef(p.id, el)"
          :backend="p.backend"
          :root-path="p.rootPath"
        />
      </div>
    </div>

    <!-- 空状态 -->
    <div v-else class="re-empty">
      <el-icon :size="40"><EditPen /></el-icon>
      <p>还没有打开的编辑器板块</p>
      <p class="sub">点击上方「打开本地文件夹」浏览本机文件，或在 FTP / SFTP 页右键目录选「用编辑器打开目录」</p>
    </div>

    <ContextMenu v-model="tabCtxVisible" :x="tabCtxX" :y="tabCtxY" :items="tabCtxItems" @pick="onTabCtxPick" />
  </div>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { EditPen, Close, FolderOpened, CloseBold, DArrowRight, CircleClose } from '@element-plus/icons-vue'
import RemoteEditorPanel from '../components/RemoteEditorPanel.vue'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import {
  remoteEditor,
  openDirInEditor,
  closePanel as closePanelStore,
  type RemotePanelInfo,
} from '../stores/remoteEditor'
import { makeLocalBackend } from '../utils/fileBackend'
import { LocalService } from '../utils/wails'

const panelRefs = ref<Record<number, InstanceType<typeof RemoteEditorPanel> | null>>({})

// 标签页右键菜单
const tabCtxVisible = ref(false)
const tabCtxX = ref(0)
const tabCtxY = ref(0)
const tabCtxItems = ref<(CtxItem | 'divider')[]>([])
const tabCtxTab = ref<RemotePanelInfo | null>(null)
const tabCtxIndex = ref(-1)

// 选择本机目录并在编辑器里打开（本地文件后端，复用同一套编辑器）
async function openLocalFolder() {
  try {
    const dir = await LocalService.PickDirectory()
    if (!dir) return
    openDirInEditor(makeLocalBackend(), dir)
  } catch (e: any) {
    ElMessage.error(`打开本地文件夹失败：${e?.message || e}`)
  }
}

function setPanelRef(id: number, el: any) {
  panelRefs.value[id] = el
}

async function closePanel(id: number) {
  const inst = panelRefs.value[id]
  if (inst && !(await inst.confirmClose())) return
  closePanelStore(id)
  delete panelRefs.value[id]
}

async function closeAll() {
  await closePanels([...remoteEditor.panels])
}

// 批量关闭：逐个询问（有未保存内容时 confirmClose 会拦截）
async function closePanels(list: RemotePanelInfo[]) {
  for (const p of list) {
    const inst = panelRefs.value[p.id]
    if (inst && !(await inst.confirmClose())) continue
    closePanelStore(p.id)
    delete panelRefs.value[p.id]
  }
}

// ---------- 标签页右键 ----------

function onTabContext(event: MouseEvent, p: RemotePanelInfo) {
  event.preventDefault()
  tabCtxTab.value = p
  tabCtxIndex.value = remoteEditor.panels.findIndex((x) => x.id === p.id)
  tabCtxItems.value = buildTabCtx()
  tabCtxX.value = event.clientX
  tabCtxY.value = event.clientY
  tabCtxVisible.value = false
  requestAnimationFrame(() => {
    tabCtxVisible.value = true
  })
}

function buildTabCtx(): (CtxItem | 'divider')[] {
  const total = remoteEditor.panels.length
  const idx = tabCtxIndex.value
  return [
    { key: 'close-current', label: '关闭当前', icon: Close, disabled: total === 0 },
    { key: 'close-others', label: '关闭其他', icon: CloseBold, disabled: total <= 1 },
    { key: 'close-right', label: '关闭右边', icon: DArrowRight, disabled: idx < 0 || idx >= total - 1 },
    { key: 'close-all', label: '关闭全部', icon: CircleClose, disabled: total === 0 },
  ]
}

async function onTabCtxPick(item: CtxItem) {
  const p = tabCtxTab.value
  if (!p) return
  const idx = remoteEditor.panels.findIndex((x) => x.id === p.id)
  switch (item.key) {
    case 'close-current':
      await closePanel(p.id)
      break
    case 'close-others':
      await closePanels(remoteEditor.panels.filter((x) => x.id !== p.id))
      break
    case 'close-right':
      await closePanels(idx >= 0 ? remoteEditor.panels.slice(idx + 1) : [])
      break
    case 'close-all':
      await closeAll()
      break
  }
}

// 双击文件 → 新板块挂载完成后打开该文件（immediate：跳转前已设置也生效）
watch(
  () => remoteEditor.pendingOpen,
  async (req) => {
    if (!req) return
    await nextTick()
    const panel = remoteEditor.panels.find((p) => p.id === remoteEditor.activeId)
    if (panel) {
      const inst = panelRefs.value[panel.id]
      if (inst) {
        await inst.openPath(req.path)
      } else {
        ElMessage.error('编辑器板块尚未就绪，请稍后重试')
      }
    }
    remoteEditor.pendingOpen = null
  },
  { immediate: true, flush: 'post' },
)
</script>

<style scoped>
.remote-editor-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
}

.re-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}

.re-hint {
  font-size: 12px;
  color: var(--text-secondary);
}

.re-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: var(--panel-bg);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  overflow: hidden;
}

.re-tabs {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 4px 6px 0;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  overflow-x: auto;
}

.re-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  font-size: 12.5px;
  color: var(--text-secondary);
  /* background: var(--panel-soft);
  border: 1px solid var(--border-color); */
  border-bottom: none;
  border-radius: 6px 6px 0 0;
  cursor: pointer;
  max-width: 220px;
  flex-shrink: 0;
  user-select: none;
}

.re-tab.active {
  background: var(--active-bg);
  color: var(--active-text);
}

.re-tab-icon {
  flex-shrink: 0;
}

.re-tab-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.re-tab-close {
  color: var(--text-secondary);
  border-radius: 3px;
  flex-shrink: 0;
}

.re-tab-close:hover {
  color: #f56c6c;
  background: rgba(245, 108, 108, 0.15);
}

.re-panels {
  flex: 1;
  min-height: 0;
  position: relative;
  padding: 8px;
}

.re-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--text-secondary);
}

.re-empty p {
  margin: 0;
  font-size: 13px;
}

.re-empty .sub {
  font-size: 12px;
  opacity: 0.75;
}
</style>
