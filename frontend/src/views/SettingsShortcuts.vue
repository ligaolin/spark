<template>
  <div>
    <div class="sc-head">
      <span class="sc-tip">在输入框、终端等可输入区域中按键不会被拦截</span>
      <el-button size="small" @click="resetAll">恢复全部默认</el-button>
    </div>
    <div class="sc-note">
      终端内复制 / 粘贴快捷键固定为 <b>Ctrl+Shift+C</b> / <b>Ctrl+Shift+V</b>（亦支持 Ctrl+Insert / Shift+Insert）；
      Ctrl+C 保留为中断信号，Ctrl+V 走系统原生粘贴。
    </div>
    <div class="sc-list">
      <div v-for="item in shortcuts.items" :key="item.id" class="sc-row">
        <div class="sc-info">
          <div class="sc-label">{{ item.label }}</div>
          <div class="sc-desc">{{ item.hint }}</div>
        </div>
        <div class="sc-ctrl">
          <el-tag
            class="sc-key"
            :class="{ capturing: capturing === item.id }"
            @click="startCapture(item.id)"
          >
            {{ capturing === item.id ? '按下新的快捷键…' : item.key }}
          </el-tag>
          <el-button size="small" @click="shortcuts.reset(item.id)">恢复默认</el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onBeforeUnmount, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useShortcutsStore, eventToCombo } from '../stores/shortcuts'

const shortcuts = useShortcutsStore()

const capturing = ref<string | null>(null)
let captureHandler: ((e: KeyboardEvent) => void) | null = null

onMounted(async () => {
  await shortcuts.load()
})

function startCapture(id: string) {
  stopCapture()
  capturing.value = id
  captureHandler = (e: KeyboardEvent) => {
    e.preventDefault()
    e.stopPropagation()
    if (e.key === 'Escape') {
      stopCapture()
      return
    }
    const combo = eventToCombo(e)
    if (!combo) return
    stopCapture()
    shortcuts.bind(id, combo).then((err) => {
      if (err) ElMessage.error(err)
      else ElMessage.success('已设置')
    })
  }
  window.addEventListener('keydown', captureHandler, true)
}

function stopCapture() {
  if (captureHandler) {
    window.removeEventListener('keydown', captureHandler, true)
    captureHandler = null
  }
  capturing.value = null
}

async function resetAll() {
  await shortcuts.resetAll()
  ElMessage.success('已恢复全部默认快捷键')
}

onBeforeUnmount(stopCapture)
</script>

<style scoped>
.sc-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.sc-tip {
  font-size: 12px;
  color: var(--text-secondary);
}

.sc-note {
  font-size: 12px;
  color: var(--text-secondary);
  background: #1e222b;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 8px 12px;
  margin-bottom: 10px;
  line-height: 1.7;
}

.sc-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: 720px;
}

.sc-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  background: var(--panel-bg);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 10px 14px;
}

.sc-info {
  min-width: 0;
}

.sc-label {
  font-size: 13.5px;
  font-weight: 600;
}

.sc-desc {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 2px;
}

.sc-ctrl {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.sc-key {
  cursor: pointer;
  min-width: 110px;
  justify-content: center;
  user-select: none;
}

.sc-key.capturing {
  border-color: var(--accent);
  color: var(--accent);
  animation: pulse 1s infinite;
}

@keyframes pulse {
  50% {
    opacity: 0.5;
  }
}
</style>