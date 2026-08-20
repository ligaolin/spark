<template>
  <el-dialog
    :model-value="modelValue"
    title="导入 SSH Config"
    width="720px"
    :close-on-click-modal="false"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="ssh-import">
      <div class="src-row">
        <el-input
          v-model="content"
          type="textarea"
          :rows="8"
          placeholder="粘贴 ~/.ssh/config 内容，或点击「选择文件」读取配置文件"
          spellcheck="false"
        />
      </div>
      <div class="toolbar">
        <div class="toolbar-left">
          <el-button size="small" @click="pickFile">选择文件</el-button>
          <el-button size="small" @click="loadDefault">读取默认配置</el-button>
          <el-button size="small" type="primary" :loading="parsing" @click="doParse">解析</el-button>
        </div>
        <div class="toolbar-right">
          <span class="target-label">导入到分组</span>
          <el-select
            v-model="group"
            size="small"
            filterable
            allow-create
            clearable
            default-first-option
            placeholder="未分组"
            style="width: 160px"
          >
            <el-option v-for="g in groups" :key="g" :label="g" :value="g" />
          </el-select>
        </div>
      </div>

      <div class="list-head">
        <span>解析结果（{{ hosts.length }} 台主机）</span>
        <span v-if="selected.length" class="sel-note">已选 {{ selected.length }} 台</span>
      </div>
      <el-table
        :data="hosts"
        size="small"
        height="240"
        empty-text="解析后此处显示可导入的主机"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="42" />
        <el-table-column label="Host（名称）">
          <template #default="{ row }">
            <span class="host-name">{{ row.name }}</span>
          </template>
        </el-table-column>
        <el-table-column label="HostName（地址）">
          <template #default="{ row }">
            <span class="mono dim">{{ row.hostName }}</span>
          </template>
        </el-table-column>
        <el-table-column label="用户">
          <template #default="{ row }">
            <span class="dim">{{ row.user || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="端口" width="70">
          <template #default="{ row }">
            <span class="dim">{{ row.port || 22 }}</span>
          </template>
        </el-table-column>
        <el-table-column label="认证" width="70">
          <template #default="{ row }">
            <el-tag v-if="row.identityFile" size="small" type="info">密钥</el-tag>
            <el-tag v-else size="small" type="info" effect="plain">密码</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button :disabled="!selected.length" :loading="importing" @click="doImport(selected)">
        导入选中（{{ selected.length }}）
      </el-button>
      <el-button type="primary" :disabled="!hosts.length" :loading="importing" @click="doImport(hosts)">
        全部导入（{{ hosts.length }}）
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { SshConfigService, LocalService } from '../utils/wails'
import type { SshHost } from '../utils/wails'

const props = defineProps<{
  modelValue: boolean
  groups: string[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'imported'): void
}>()

const content = ref('')
const hosts = ref<SshHost[]>([])
const selected = ref<SshHost[]>([])
const group = ref('')
const parsing = ref(false)
const importing = ref(false)
let defaultLoaded = false

watch(
  () => props.modelValue,
  (v) => {
    if (!v) return
    if (!content.value && !defaultLoaded) {
      loadDefault()
    }
  },
)

function onSelectionChange(rows: SshHost[]) {
  selected.value = rows
}

async function pickFile() {
  try {
    const p = await LocalService.PickOpenFile('选择 SSH Config 文件')
    if (!p) return
    content.value = await LocalService.ReadTextFile(p)
    await doParse()
  } catch (e: any) {
    ElMessage.error(`读取文件失败：${e?.message || e}`)
  }
}

async function loadDefault() {
  defaultLoaded = true
  try {
    const p = await SshConfigService.DefaultConfigPath()
    const txt = await LocalService.ReadTextFile(p)
    if (txt) {
      content.value = txt
      await doParse()
    }
  } catch {
    // 没有默认 config 文件，忽略，等用户粘贴或选择文件
  }
}

async function doParse() {
  const text = content.value.trim()
  if (!text) {
    ElMessage.warning('请先粘贴或读取 SSH Config 内容')
    return
  }
  parsing.value = true
  try {
    hosts.value = (await SshConfigService.Parse(text)) ?? []
    selected.value = []
    if (!hosts.value.length) {
      ElMessage.warning('未解析到可导入的主机（需包含 Host 与 HostName）')
    }
  } catch (e: any) {
    ElMessage.error(`解析失败：${e?.message || e}`)
  } finally {
    parsing.value = false
  }
}

async function doImport(list: SshHost[]) {
  if (!list.length) {
    ElMessage.warning('请先选择要导入的主机')
    return
  }
  importing.value = true
  try {
    const res = await SshConfigService.Import(list, group.value || '')
    const warnings = res?.warnings || []
    ElMessage.success(`已导入 ${res?.imported ?? 0} 个连接`)
    if (warnings.length) {
      ElMessage.warning(warnings.join('；'))
    }
    emit('imported')
    emit('update:modelValue', false)
  } catch (e: any) {
    ElMessage.error(`导入失败：${e?.message || e}`)
  } finally {
    importing.value = false
  }
}
</script>

<style scoped>
.ssh-import {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.src-row :deep(.el-textarea__inner) {
  font-family: 'Cascadia Code', Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.toolbar-left {
  display: flex;
  gap: 8px;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.target-label {
  font-size: 13px;
  color: var(--text-secondary);
}

.list-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 13px;
  color: var(--text-secondary);
}

.sel-note {
  color: #7fb0ff;
}

.host-name {
  font-weight: 500;
}

.mono {
  font-family: 'Cascadia Code', Consolas, monospace;
}

.dim {
  color: var(--text-secondary);
}
</style>
