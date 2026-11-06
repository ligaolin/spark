<template>
  <el-dialog :model-value="modelValue" title="修改权限" width="640px" :close-on-click-modal="false"
    @update:model-value="close">
    <div class="perm-dialog">
      <div class="perm-path" :title="path">{{ path }}</div>

      <div class="perm-groups">
        <fieldset v-for="g in groups" :key="g.key" class="perm-group">
          <legend>{{ g.label }}</legend>
          <el-checkbox v-model="bits[g.key].r">读取</el-checkbox>
          <el-checkbox v-model="bits[g.key].w">写入</el-checkbox>
          <el-checkbox v-model="bits[g.key].x">执行</el-checkbox>
        </fieldset>
      </div>

      <div class="perm-bottom">
        <el-input v-model="octal" class="perm-octal" maxlength="4" @change="onOctalChange" />
        <span class="perm-text">权限，所有者</span>
        <el-select :model-value="owner || '当前用户'" class="perm-owner" disabled>
          <el-option :label="owner || '当前用户'" :value="owner || '当前用户'" />
        </el-select>
        <el-checkbox v-model="recursive" :disabled="!isDir">应用到子目录</el-checkbox>
        <el-button type="primary" :loading="applying" @click="apply">应用</el-button>
      </div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  bitsToOctal,
  formatOctal,
  octalToBits,
  parseMode,
  type PermBits,
} from '../utils/permissions'

const props = defineProps<{
  modelValue: boolean
  path: string
  // 当前权限：rwx 字符串（如 -rw-r--r--）或八进制
  mode?: string
  owner?: string
  isDir?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'apply', payload: { mode: number; recursive: boolean }): void
}>()

const bits = reactive<PermBits>(parseMode(props.mode))
const octal = ref(formatOctal(bitsToOctal(bits)))
const recursive = ref(false)
const applying = ref(false)

const groups: Array<{ key: keyof PermBits; label: string }> = [
  { key: 'owner', label: '所有者' },
  { key: 'group', label: '用户组' },
  { key: 'public', label: '公共' },
]

let syncing = false

// 勾选矩阵 → 八进制
watch(
  bits,
  () => {
    if (syncing) return
    syncing = true
    octal.value = formatOctal(bitsToOctal(bits))
    syncing = false
  },
  { deep: true },
)

// 八进制 → 勾选矩阵
function onOctalChange(v: string) {
  if (syncing) return
  const raw = v.trim()
  if (!/^[0-7]{1,4}$/.test(raw)) {
    ElMessage.warning('请输入八进制权限，如 755')
    octal.value = formatOctal(bitsToOctal(bits))
    return
  }
  syncing = true
  Object.assign(bits, octalToBits(parseInt(raw, 8)))
  octal.value = formatOctal(bitsToOctal(bits))
  syncing = false
}

function reset() {
  const parsed = parseMode(props.mode)
  syncing = true
  Object.assign(bits, parsed)
  octal.value = formatOctal(bitsToOctal(parsed))
  syncing = false
  recursive.value = false
  applying.value = false
}

watch(
  () => props.modelValue,
  (v) => {
    if (v) reset()
  },
)

function close(v: boolean) {
  emit('update:modelValue', v)
}

function apply() {
  applying.value = true
  emit('apply', { mode: bitsToOctal(bits), recursive: recursive.value && !!props.isDir })
}
</script>

<style scoped>
.perm-dialog {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.perm-path {
  font-family: var(--term-font);
  font-size: 12px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.perm-groups {
  display: flex;
  gap: 14px;
}

.perm-group {
  flex: 1;
  min-width: 0;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 8px 12px 10px;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.perm-group legend {
  padding: 0 6px;
  font-size: 12px;
  color: var(--text-secondary);
}

.perm-group :deep(.el-checkbox) {
  margin-right: 0;
  height: 22px;
}

.perm-bottom {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.perm-octal {
  width: 82px;
}

.perm-text {
  font-size: 12.5px;
  color: var(--text-secondary);
}

.perm-owner {
  width: 130px;
}
</style>
