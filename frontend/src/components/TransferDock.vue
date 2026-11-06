<template>
  <div class="transfer-dock" v-if="items.length > 0">
    <div class="dock-head">
      <span>{{ title }}（{{ items.length }}）</span>
      <div class="dock-head-actions">
        <el-button size="small" text @click="store.clear(dockScope)">清空</el-button>
      </div>
    </div>
    <div class="dock-body">
      <div v-for="item in items" :key="item.key" class="tf-item">
        <span class="tf-icon" :class="item.status">
          <el-icon v-if="item.status === 'running'">
            <Loading class="is-loading" />
          </el-icon>
          <el-icon v-else-if="item.status === 'done'"><CircleCheckFilled /></el-icon>
          <el-icon v-else><CircleCloseFilled /></el-icon>
        </span>
        <div class="tf-main">
          <div class="tf-line">
            <span class="tf-name" :title="item.label">{{ item.label }}</span>
            <span class="tf-meta">
              {{ opLabel(item.op) }}
              <template v-if="item.fileTotal > 0">
                · {{ item.fileDone }}/{{ item.fileTotal }}
              </template>
              <template v-if="item.totalBytes > 0">
                · {{ formatSize(item.doneBytes) }}/{{ formatSize(item.totalBytes) }}
              </template>
            </span>
          </div>
          <el-progress
            :percentage="item.percent"
            :stroke-width="6"
            :status="item.status === 'error' ? 'exception' : item.status === 'done' ? 'success' : undefined"
          />
          <div v-if="item.error" class="tf-err" :title="item.error">{{ item.error }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Loading, CircleCheckFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import { useTransfersStore, type TransferKind } from '../stores/transfers'
import { formatSize } from '../types'

const props = defineProps<{
  // 只展示本页面发起的任务（SFTP 终端 / FTP / 独立文件页互不串台）
  scope?: string | string[]
  kind?: TransferKind | TransferKind[]
  title?: string
}>()

const store = useTransfersStore()

const dockScope = computed(() => (Array.isArray(props.scope) ? props.scope[0] : props.scope))

const items = computed(() => {
  const scopes = props.scope
    ? Array.isArray(props.scope)
      ? new Set(props.scope)
      : new Set([props.scope])
    : null
  const kinds = props.kind
    ? Array.isArray(props.kind)
      ? new Set(props.kind)
      : new Set([props.kind])
    : null
  return store.tasks.filter(
    (t) => (!scopes || scopes.has(t.scope)) && (!kinds || kinds.has(t.kind)),
  )
})

function opLabel(op: string): string {
  switch (op) {
    case 'upload':
      return '上传'
    case 'download':
      return '下载'
    case 'delete':
      return '删除'
    case 'copy':
      return '复制'
    default:
      return op
  }
}
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
  flex-shrink: 0;
}

.dock-head-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}

.dock-body {
  overflow-y: auto;
  padding: 6px 10px 8px;
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
  gap: 8px;
  font-size: 12px;
  margin-bottom: 3px;
}

.tf-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tf-meta {
  color: var(--text-secondary);
  flex-shrink: 0;
  font-size: 11.5px;
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
