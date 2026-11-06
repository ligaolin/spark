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
        <el-input v-model="octal" class="perm-octal" maxlength="4" @input="onOctalInput"
          @change="onOctalChange" />
        <span class="perm-text">权限，所有者</span>
        <el-select v-model="ownerUid" class="perm-owner" filterable :loading="ownerLoading"
          :disabled="!canChangeOwner" :placeholder="owner || '保持不变'">
          <el-option :label="owner ? `保持不变（${owner}）` : '保持不变'" :value="-1" />
          <el-option v-for="u in users" :key="u.name" :label="`${u.name}（${u.uid}）`" :value="u.uid" />
        </el-select>
        <el-checkbox v-model="recursive" :disabled="!isDir">应用到子目录</el-checkbox>
        <el-button type="primary" :loading="applying" @click="apply">应用</el-button>
      </div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  bitsToOctal,
  formatOctal,
  octalToBits,
  parseMode,
  type PermBits,
} from '../utils/permissions'
import type { OSUser } from '../utils/wails'

const props = defineProps<{
  modelValue: boolean
  path: string
  // 当前权限：rwx 字符串（如 -rw-r--r--）或八进制
  mode?: string
  // 当前所有者名称；系统账号列表为空时下拉只读
  owner?: string
  users?: OSUser[]
  ownerLoading?: boolean
  isDir?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'apply', payload: { mode: number; recursive: boolean; uid: number; gid: number }): void
}>()

const bits = reactive<PermBits>(parseMode(props.mode))
const octal = ref(formatOctal(bitsToOctal(bits)))
const recursive = ref(false)
const applying = ref(false)
// -1 = 保持不变；否则为新所有者的 uid
const ownerUid = ref(-1)

const canChangeOwner = computed(() => !!props.users?.length)

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

// 输入过程中即时更新上面的勾选；这里不重写输入框内容，避免打断连续输入
function onOctalInput(v: string) {
  if (syncing) return
  const raw = (v ?? '').trim()
  if (!/^[0-7]{1,4}$/.test(raw)) return
  syncing = true
  Object.assign(bits, octalToBits(parseInt(raw, 8)))
  syncing = false
}

// 八进制 → 勾选矩阵（失焦/回车时校验并规范为三位）
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
  ownerUid.value = -1
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
  const user = props.users?.find((u) => u.uid === ownerUid.value)
  emit('apply', {
    mode: bitsToOctal(bits),
    recursive: recursive.value && !!props.isDir,
    uid: user ? user.uid : -1,
    gid: user ? user.gid : -1,
  })
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
  width: 170px;
}
</style>
