<template>
  <teleport to="body">
    <div
      v-if="visible"
      class="ctx-overlay"
      @mousedown="close"
      @wheel="close"
      @contextmenu.prevent="close"
      @keydown.esc="close"
    >
      <div
        class="ctx-menu"
        :style="{ left: pos.x + 'px', top: pos.y + 'px' }"
        @mousedown.stop
        @contextmenu.stop
      >
        <template v-for="(item, i) in items" :key="i">
          <div v-if="item === 'divider'" class="ctx-divider" />
          <div
            v-else
            class="ctx-item"
            :class="{ danger: item.danger, disabled: item.disabled }"
            @click="pick(item)"
          >
            <el-icon v-if="item.icon" class="ctx-icon"><component :is="item.icon" /></el-icon>
            <span>{{ item.label }}</span>
            <span v-if="item.hint" class="ctx-hint">{{ item.hint }}</span>
          </div>
        </template>
      </div>
    </div>
  </teleport>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'

export interface CtxItem {
  key: string
  label: string
  icon?: any
  danger?: boolean
  disabled?: boolean
  hint?: string
}

const props = defineProps<{
  modelValue: boolean
  x: number
  y: number
  items: (CtxItem | 'divider')[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'pick', item: CtxItem): void
}>()

const visible = ref(props.modelValue)
const pos = reactive({ x: 0, y: 0 })

watch(
  () => props.modelValue,
  (v) => {
    visible.value = v
    if (v) {
      // 菜单尺寸估算：条目数 * 30 + 边框，夹在视口内
      const count = props.items.filter((i) => i !== 'divider').length
      const w = 190
      const h = count * 30 + 12
      pos.x = Math.min(props.x, window.innerWidth - w - 8)
      pos.y = Math.min(props.y, window.innerHeight - h - 8)
      if (pos.x < 8) pos.x = 8
      if (pos.y < 8) pos.y = 8
    }
  },
)

function close() {
  visible.value = false
  emit('update:modelValue', false)
}

function pick(item: CtxItem) {
  if (item.disabled) return
  close()
  emit('pick', item)
}
</script>

<style scoped>
.ctx-overlay {
  position: fixed;
  inset: 0;
  z-index: 3000;
}

.ctx-menu {
  position: fixed;
  min-width: 180px;
  padding: 5px;
  background: var(--menu-bg);
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  user-select: none;
}

.ctx-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border-radius: 5px;
  font-size: 13px;
  color: var(--text-primary);
  cursor: pointer;
  white-space: nowrap;
}

.ctx-item:hover:not(.disabled) {
  background: var(--hover-strong);
}

.ctx-item.danger {
  color: #f56c6c;
}

.ctx-item.danger:hover:not(.disabled) {
  background: rgba(245, 108, 108, 0.12);
}

.ctx-item.disabled {
  color: var(--text-muted);
  cursor: not-allowed;
}

.ctx-icon {
  font-size: 14px;
  color: var(--text-secondary);
}

.ctx-item.danger .ctx-icon {
  color: #f56c6c;
}

.ctx-hint {
  margin-left: auto;
  font-size: 11px;
  color: var(--text-secondary);
}

.ctx-divider {
  height: 1px;
  margin: 4px 6px;
  background: var(--border-color);
}
</style>
