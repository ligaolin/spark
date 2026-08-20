<template>
  <el-dialog
    :model-value="modelValue"
    :title="title"
    width="400px"
    :close-on-click-modal="false"
    @update:model-value="onClose"
  >
    <div class="confirm-body">
      <el-icon class="confirm-icon" :size="22" :color="danger ? '#f56c6c' : '#e6a23c'">
        <WarningFilled />
      </el-icon>
      <div class="confirm-message" v-html="message" />
    </div>
    <template #footer>
      <el-button v-if="showCancel" @click="cancel">取消</el-button>
      <el-button :type="danger ? 'danger' : 'primary'" @click="ok">
        {{ confirmText }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { WarningFilled } from '@element-plus/icons-vue'

const props = defineProps<{
  modelValue: boolean
  title: string
  message: string
  danger?: boolean
  confirmText?: string
  showCancel?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm'): void
  (e: 'cancel'): void
}>()

function ok() {
  emit('confirm')
}

function cancel() {
  emit('cancel')
}

function onClose(v: boolean) {
  if (!v) emit('cancel')
  emit('update:modelValue', v)
}
</script>

<style scoped>
.confirm-body {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.confirm-icon {
  margin-top: 2px;
  flex-shrink: 0;
}

.confirm-message {
  font-size: 13px;
  color: var(--text-primary);
  line-height: 1.8;
  word-break: break-all;
  user-select: text;
}
</style>
