<template>
  <el-dialog
    v-model="state.visible"
    :title="state.title"
    width="640px"
    top="8vh"
    :close-on-click-modal="false"
    @closed="closeAiResult"
  >
    <el-alert
      v-if="state.error"
      type="error"
      :closable="false"
      show-icon
      class="ai-result-error"
      :title="state.error"
    />
    <div v-if="state.streaming" class="ai-result-streaming">
      <el-icon class="is-loading"><Loading /></el-icon> 生成中…
    </div>
    <el-input
      v-model="state.text"
      type="textarea"
      :autosize="{ minRows: 10, maxRows: 24 }"
      :readonly="state.streaming"
      class="ai-result-input"
    />
    <template #footer>
      <div class="ai-result-actions">
        <el-button @click="copy">复制</el-button>
        <el-button v-if="state.streaming" type="danger" @click="stop">停止</el-button>
        <el-button
          v-if="state.replaceLabel"
          :loading="state.loading"
          :disabled="state.streaming || !state.text"
          type="warning"
          @click="applyReplace"
        >
          {{ state.replaceLabel }}
        </el-button>
        <el-button
          v-if="state.insertLabel"
          :loading="state.loading"
          :disabled="state.streaming || !state.text"
          type="primary"
          @click="applyInsert"
        >
          {{ state.insertLabel }}
        </el-button>
        <el-button @click="closeAiResult">关闭</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { Clipboard } from '@wailsio/runtime'
import { Loading } from '@element-plus/icons-vue'
import { aiResultState as state, closeAiResult } from '../utils/aiDialog'

async function copy() {
  try {
    await Clipboard.SetText(state.text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败')
  }
}

function stop() {
  state.cancel?.()
}

async function applyInsert() {
  if (!state.onInsert) return
  state.loading = true
  try {
    await state.onInsert(state.text)
    closeAiResult()
  } catch (e: any) {
    ElMessage.error(e?.message || String(e))
  } finally {
    state.loading = false
  }
}

async function applyReplace() {
  if (!state.onReplace) return
  state.loading = true
  try {
    await state.onReplace(state.text)
    closeAiResult()
  } catch (e: any) {
    ElMessage.error(e?.message || String(e))
  } finally {
    state.loading = false
  }
}
</script>

<style scoped>
.ai-result-streaming {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-secondary);
  font-size: 13px;
  margin-bottom: 8px;
}

.ai-result-error {
  margin-bottom: 8px;
}

.ai-result-input :deep(.el-textarea__inner) {
  font-family: var(--term-font);
  font-size: 13px;
  line-height: 1.6;
}

.ai-result-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
