<template>
  <div class="si-view">
    <div class="si-toolbar">
      <el-button size="small" type="primary" plain :loading="loading" @click="load">
        <el-icon style="margin-right: 4px"><Refresh /></el-icon>
        刷新
      </el-button>
      <el-tag v-if="info?.error" size="small" type="warning" class="si-warn">{{ info.error }}</el-tag>
      <el-tag v-if="info && !info.error" size="small" type="success" effect="dark">已连接</el-tag>
    </div>

    <template v-if="info">
      <div class="si-grid">
        <div class="si-cell">
          <div class="si-label">主机名</div>
          <div class="si-value" :title="info.hostname">{{ info.hostname || '—' }}</div>
        </div>
        <div class="si-cell">
          <div class="si-label">操作系统</div>
          <div class="si-value" :title="info.os">{{ info.os || '—' }}</div>
        </div>
        <div class="si-cell">
          <div class="si-label">内核</div>
          <div class="si-value" :title="info.kernel">{{ info.kernel || '—' }}</div>
        </div>
        <div class="si-cell">
          <div class="si-label">架构</div>
          <div class="si-value">{{ info.arch || '—' }}</div>
        </div>
        <div class="si-cell">
          <div class="si-label">运行时间</div>
          <div class="si-value">{{ info.uptime || '—' }}</div>
        </div>
      </div>

      <div class="si-block">
        <div class="si-block-title">CPU</div>
        <div class="si-row">
          <span class="si-label">型号</span>
          <span class="si-value" :title="info.cpuModel">{{ info.cpuModel || '—' }}</span>
        </div>
        <div class="si-row">
          <span class="si-label">核心数</span>
          <span class="si-value">{{ info.cpuCores || '—' }}</span>
        </div>
        <div class="si-row">
          <span class="si-label">负载 (1/5/15)</span>
          <span class="si-value mono">{{ loadText }}</span>
        </div>
      </div>

      <div class="si-block">
        <div class="si-block-title">内存</div>
        <el-progress
          :percentage="memPercent"
          :stroke-width="8"
          :color="memPercent > 90 ? '#f56c6c' : memPercent > 70 ? '#e6a23c' : '#4f8cff'"
        />
        <div class="si-sub">已用 {{ formatSize(memUsed) }} / 共 {{ formatSize(memTotal) }}</div>
      </div>

      <div class="si-block">
        <div class="si-block-title">磁盘</div>
        <template v-if="info.disks?.length">
          <div v-for="(d, i) in info.disks" :key="i" class="si-disk">
            <div class="si-disk-head">
              <span class="si-value" :title="d.mount">{{ d.mount }}</span>
              <span class="si-sub">{{ d.usePercent }}%</span>
            </div>
            <el-progress
              :percentage="d.usePercent"
              :stroke-width="6"
              :color="d.usePercent > 90 ? '#f56c6c' : d.usePercent > 70 ? '#e6a23c' : '#4f8cff'"
            />
            <div class="si-sub">
              已用 {{ formatSize(d.used) }} / {{ formatSize(d.size) }}，可用 {{ formatSize(d.avail) }}
            </div>
          </div>
        </template>
        <div v-else class="si-sub">暂无磁盘信息</div>
      </div>
    </template>

    <el-empty v-else-if="!loading" description="暂无数据，点击刷新获取" :image-size="60" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { TerminalService } from '../utils/wails'
import type { ServerInfo } from '../utils/wails'
import { formatSize } from '../types'

const props = defineProps<{ sessionId: string }>()

const loading = ref(false)
const info = ref<ServerInfo | null>(null)

const memPercent = computed(() => {
  if (!info.value || !info.value.memoryTotal) return 0
  return Math.min(100, Math.round((info.value.memoryUsed / info.value.memoryTotal) * 100))
})
const memTotal = computed(() => info.value?.memoryTotal || 0)
const memUsed = computed(() => info.value?.memoryUsed || 0)
const loadText = computed(() => {
  const i = info.value
  if (!i) return ''
  return `${i.load1?.toFixed(2) ?? '—'} / ${i.load5?.toFixed(2) ?? '—'} / ${i.load15?.toFixed(2) ?? '—'}`
})

async function load() {
  if (!props.sessionId) return
  loading.value = true
  try {
    info.value = await TerminalService.ServerInfo(props.sessionId)
  } catch (e: any) {
    ElMessage.error(`获取服务器信息失败：${e?.message || e}`)
  } finally {
    loading.value = false
  }
}

watch(
  () => props.sessionId,
  (v) => {
    if (v) load()
  },
  { immediate: true },
)
</script>

<style scoped>
.si-view {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 10px;
  overflow-y: auto;
  height: 100%;
}

.si-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.si-warn {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.si-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.si-cell {
  background: #1e222b;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 8px 10px;
  min-width: 0;
}

.si-label {
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 3px;
}

.si-value {
  font-size: 13px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.si-block {
  background: #1e222b;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.si-block-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--accent);
  margin-bottom: 2px;
}

.si-row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: baseline;
}

.si-sub {
  font-size: 11px;
  color: var(--text-secondary);
}

.si-disk {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.si-disk-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
}

.mono {
  font-family: var(--term-font);
}
</style>
