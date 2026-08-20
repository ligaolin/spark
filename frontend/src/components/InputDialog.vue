<template>
  <el-dialog
    :model-value="modelValue"
    :title="title"
    width="430px"
    :close-on-click-modal="false"
    @update:model-value="onClose"
  >
    <div class="input-dialog-body">
      <div v-for="f in fields" :key="f.key" class="id-field">
        <div class="id-label">{{ f.label }}</div>
        <el-input
          v-model="values[f.key]"
          :placeholder="f.placeholder"
          :type="f.type || 'text'"
          @keyup.enter="confirm"
        />
      </div>
    </div>
    <template #footer>
      <el-button @click="cancel">取消</el-button>
      <el-button type="primary" @click="confirm">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { nextTick, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { DialogField } from '../utils/dialog'

const props = defineProps<{
  modelValue: boolean
  title: string
  fields: DialogField[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm', values: Record<string, string>): void
  (e: 'cancel'): void
}>()

const values = reactive<Record<string, string>>({})
const firstInput = ref<HTMLInputElement>()

watch(
  () => props.modelValue,
  (v) => {
    if (!v) return
    for (const f of props.fields) {
      values[f.key] = f.initial ?? ''
    }
    nextTick(() => {
      const el = document.querySelector<HTMLInputElement>('.input-dialog-body .el-input__inner')
      el?.focus()
    })
  },
)

function confirm() {
  for (const f of props.fields) {
    if (!(values[f.key] ?? '').trim()) {
      ElMessage.warning(`请填写「${f.label}」`)
      return
    }
  }
  emit('confirm', { ...values })
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
.input-dialog-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.id-label {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 5px;
}
</style>
