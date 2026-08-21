<template>
  <el-dialog v-model="visible" class="search-dialog" width="820px" top="8vh" :close-on-click-modal="false">
    <template #header>
      <div class="search-header">
        <el-icon><Search /></el-icon>
        <span>搜索</span>
        <span class="search-root" :title="root">从 {{ root }} 递归搜索</span>
      </div>
    </template>

    <div class="search-bar">
      <el-input
        ref="keywordRef"
        v-model="keyword"
        placeholder="输入搜索关键字"
        clearable
        style="width: 300px"
        @keyup.enter="doSearch"
      />
      <el-radio-group v-model="mode">
        <el-radio-button value="name">文件名</el-radio-button>
        <el-radio-button value="content">文件内容</el-radio-button>
      </el-radio-group>
      <el-button type="primary" :loading="searching" @click="doSearch">搜索</el-button>
    </div>

    <el-table
      :data="results"
      height="440"
      size="small"
      empty-text="输入关键字后点击「搜索」"
      @row-dblclick="onPick"
    >
      <el-table-column label="名称" min-width="200">
        <template #default="{ row }">
          <el-icon v-if="row.isDir" color="#e6c06c"><Folder /></el-icon>
          <el-icon v-else color="var(--text-secondary)"><Document /></el-icon>
          <span class="res-name" :title="row.name">{{ row.name }}</span>
        </template>
      </el-table-column>
      <el-table-column label="路径" prop="path" min-width="240" show-overflow-tooltip />
      <el-table-column v-if="mode === 'content'" label="行" width="56" align="right">
        <template #default="{ row }"><span class="dim">{{ row.lineNo }}</span></template>
      </el-table-column>
      <el-table-column v-if="mode === 'content'" label="匹配内容" min-width="240" show-overflow-tooltip>
        <template #default="{ row }"><span class="dim">{{ row.line }}</span></template>
      </el-table-column>
      <el-table-column label="大小" width="90" align="right">
        <template #default="{ row }">
          <span class="dim">{{ row.isDir ? '-' : formatSize(row.size) }}</span>
        </template>
      </el-table-column>
    </el-table>

    <div class="search-status">
      <span>{{ statusText }}</span>
      <span v-if="mode === 'content'" class="hint">内容搜索会跳过二进制文件及超过 10MB 的文件</span>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Folder, Document, Search } from '@element-plus/icons-vue'
import { formatSize } from '../types'
import type { SearchResult } from '../types'
import type { FileBackend } from '../utils/fileBackend'

const props = defineProps<{ backend: FileBackend }>()

const emit = defineEmits<{
  (e: 'pick', result: SearchResult): void
}>()

const visible = ref(false)
const keyword = ref('')
const mode = ref<'name' | 'content'>('name')
const searching = ref(false)
const results = ref<SearchResult[]>([])
const root = ref('')
const statusText = ref('')
const keywordRef = ref()

function open(rootDir: string) {
  root.value = rootDir
  results.value = []
  statusText.value = ''
  visible.value = true
  nextTick(() => keywordRef.value?.focus())
}

async function doSearch() {
  const kw = keyword.value.trim()
  if (!kw) {
    ElMessage.warning('请输入搜索关键字')
    return
  }
  searching.value = true
  statusText.value = ''
  try {
    results.value = await props.backend.search(root.value, kw, mode.value)
    statusText.value =
      results.value.length > 0 ? `共 ${results.value.length} 条结果` : '没有匹配结果'
  } catch (e: any) {
    ElMessage.error(`搜索失败：${e?.message || e}`)
    statusText.value = ''
  } finally {
    searching.value = false
  }
}

function onPick(row: SearchResult) {
  visible.value = false
  emit('pick', row)
}

defineExpose({ open })
</script>

<style scoped>
.search-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.search-root {
  font-family: var(--term-font);
  font-size: 12px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 520px;
}

.search-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.res-name {
  margin-left: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dim {
  color: var(--text-secondary);
  font-size: 12px;
}

.search-status {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 8px;
  font-size: 12px;
  color: var(--text-secondary);
  min-height: 18px;
}

.hint {
  color: var(--text-secondary);
  opacity: 0.8;
}
</style>
