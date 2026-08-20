<template>
  <div class="transfer-dock" v-if="store.items.length > 0">
    <div class="dock-head">
      <span>传输队列（{{ store.items.length }}）</span>
      <el-button size="small" text @click="store.clear()">清空</el-button>
    </div>
    <div class="dock-body">
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
import { Loading, CircleCheckFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import { useTransfersStore } from '../stores/transfers'

const store = useTransfersStore()
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
