<template>
  <div class="transfer-dock" v-if="store.items.length > 0">
    <div class="dock-head">
      <span>传输队列（{{ store.items.length }}）</span>
      <div class="dock-head-actions">
        <el-button v-if="!stick" size="small" text type="primary" @click="scrollToLatest(true)">
          最新 ↓
        </el-button>
        <el-button size="small" text @click="store.clear()">清空</el-button>
      </div>
    </div>
    <div ref="bodyRef" class="dock-body" @scroll.passive="onScroll">
      <div v-for="item in store.items" :key="item.key" class="tf-item">
        <span class="tf-icon" :class="item.status">
          <el-icon v-if="item.status === 'running'">
            <Loading class="is-loading" />
          </el-icon>
          <el-icon v-else-if="item.status === 'done'"><CircleCheckFilled /></el-icon>
          <el-icon v-else><CircleCloseFilled /></el-icon>
        </span>
        <div class="tf-main">
          <div class="tf-line">
            <span class="tf-name" :title="item.name">{{ item.name }}</span>
            <span class="tf-op">{{ item.op === 'upload' ? '上传' : '下载' }}</span>
          </div>
          <el-progress
            :percentage="item.percent"
            :stroke-width="5"
            :status="item.status === 'error' ? 'exception' : item.status === 'done' ? 'success' : undefined"
          />
          <div v-if="item.status === 'error'" class="tf-err">{{ item.error }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Loading, CircleCheckFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import { useTransfersStore } from '../stores/transfers'

const store = useTransfersStore()

const bodyRef = ref<HTMLElement | null>(null)
// 是否贴在底部：用户手动往上滚查看历史时置 false，滚回底部自动恢复
const stick = ref(true)

// 进度推进的指纹：百分比 / 状态变化时触发，用于持续跟随最新一条
const progressSig = computed(() =>
  store.items.map((i) => `${i.key}:${i.percent}:${i.status}`).join('|'),
)

function isAtBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= 24
}

function onScroll() {
  const el = bodyRef.value
  if (el) stick.value = isAtBottom(el)
}

// 滚到最新一条（force=true 时即使用户已上滑也拉回底部）；用 RAF 合并同一帧的高频调用
let scrollRafId: number | null = null

function scrollToLatest(force = false) {
  if (force) stick.value = true
  if (!stick.value) return
  if (scrollRafId !== null) return
  scrollRafId = requestAnimationFrame(() => {
    scrollRafId = null
    const el = bodyRef.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

// 有新条目就滚到最后，确保始终能看到最新上传进度；已在底部时进度更新也继续跟随
watch(() => store.items.length, () => scrollToLatest(true))
watch(progressSig, () => scrollToLatest())

onMounted(() => scrollToLatest(true))
onBeforeUnmount(() => {
  if (scrollRafId !== null) cancelAnimationFrame(scrollRafId)
})
</script>

<style scoped>
.transfer-dock {
  flex-shrink: 0;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--panel-bg);
  max-height: 180px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.dock-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 10px;
  font-size: 12px;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--border-color);
}

.dock-head-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}

.dock-body {
  overflow-y: auto;
  padding: 4px 10px 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tf-item {
  display: flex;
  gap: 10px;
  align-items: flex-start;
}

.tf-icon.running {
  color: var(--accent);
}
.tf-icon.done {
  color: #34c759;
}
.tf-icon.error {
  color: #f56c6c;
}

.tf-main {
  flex: 1;
  min-width: 0;
}

.tf-line {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  margin-bottom: 2px;
}

.tf-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tf-op {
  color: var(--text-secondary);
  flex-shrink: 0;
}

.tf-err {
  font-size: 11px;
  color: #f56c6c;
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>