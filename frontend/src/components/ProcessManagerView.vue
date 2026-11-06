<template>
  <div class="pm-view">
    <div class="pm-toolbar">
      <el-input
        v-model="keyword"
        size="small"
        placeholder="搜索 PID / 用户 / 命令"
        clearable
        class="pm-search"
      />
      <el-button size="small" :loading="loading" @click="refresh">刷新</el-button>
      <el-switch v-model="auto" size="small" active-text="自动" />
    </div>

    <div class="pm-table">
      <el-table :data="filtered" size="small" height="100%" empty-text="暂无进程数据"
        :default-sort="{ prop: 'cpu', order: 'descending' }" @sort-change="onSortChange" @row-click="select">
        <el-table-column label="PID" width="70" prop="pid" sortable="custom">
          <template #default="{ row }">
            <span class="mono">{{ row.pid }}</span>
          </template>
        </el-table-column>
        <el-table-column label="用户" width="70" prop="user" sortable="custom" show-overflow-tooltip />
        <el-table-column label="CPU%" width="55" align="right" prop="cpu" sortable="custom">
          <template #default="{ row }">
            <span class="mono" :class="{ hot: row.cpu > 50 }">{{ row.cpu.toFixed(1) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="MEM%" width="60" align="right" prop="mem" sortable="custom">
          <template #default="{ row }">
            <span class="mono">{{ row.mem.toFixed(1) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="内存" width="75" align="right" prop="rss" sortable="custom">
          <template #default="{ row }">
            <span class="mono dim">{{ formatSize((row.rss || 0) * 1024) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="55" prop="stat" sortable="custom">
          <template #default="{ row }">
            <span class="mono dim">{{ row.stat }}</span>
          </template>
        </el-table-column>
        <el-table-column label="命令" min-width="140" prop="command" sortable="custom" show-overflow-tooltip />
      </el-table>
    </div>

    <div class="pm-foot">
      <span class="pm-info">共 {{ filtered.length }} 个进程（每 {{ settings.processRefreshInterval }} 秒自动刷新）</span>
      <el-button
        size="small"
        type="danger"
        plain
        :disabled="!selectedRow"
        @click="kill"
      >
        终止 PID {{ selectedRow?.pid || '' }}
      </el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { TerminalService } from '../utils/wails'
import type { ProcessInfo } from '../utils/wails'
import { useSettingsStore } from '../stores/settings'
import { showConfirmDialog } from '../utils/dialog'
import { formatSize } from '../types'

const props = defineProps<{
  sessionId: string
  // 面板是否处于激活状态（用于控制自动刷新）
  active?: boolean
}>()

const settings = useSettingsStore()

const processes = ref<ProcessInfo[]>([])
const loading = ref(false)
const keyword = ref('')
const auto = ref(true)
const selectedRow = ref<ProcessInfo | null>(null)
let timer: ReturnType<typeof setInterval> | null = null

// 点击表头排序：默认按 CPU 降序
type SortKey = 'pid' | 'user' | 'cpu' | 'mem' | 'rss' | 'stat' | 'command'
const sortKey = ref<SortKey>('cpu')
const sortOrder = ref<'ascending' | 'descending'>('descending')

function onSortChange({ prop, order }: { prop: string | null; order: 'ascending' | 'descending' | null }) {
  if (!prop || !order) {
    sortKey.value = 'cpu'
    sortOrder.value = 'descending'
    return
  }
  sortKey.value = prop as SortKey
  sortOrder.value = order
}

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  let list = processes.value
  if (kw) {
    list = list.filter(
      (p) =>
        String(p.pid).includes(kw) ||
        (p.user || '').toLowerCase().includes(kw) ||
        (p.command || '').toLowerCase().includes(kw),
    )
  }
  const dir = sortOrder.value === 'ascending' ? 1 : -1
  return [...list].sort((a, b) => {
    switch (sortKey.value) {
      case 'pid':
        return (a.pid - b.pid) * dir
      case 'cpu':
        return (a.cpu - b.cpu) * dir
      case 'mem':
        return (a.mem - b.mem) * dir
      case 'rss':
        return ((a.rss || 0) - (b.rss || 0)) * dir
      case 'user':
        return String(a.user || '').localeCompare(String(b.user || '')) * dir
      case 'stat':
        return String(a.stat || '').localeCompare(String(b.stat || '')) * dir
      case 'command':
        return String(a.command || '').localeCompare(String(b.command || '')) * dir
      default:
        return 0
    }
  })
})

async function refresh() {
  if (!props.sessionId) return
  loading.value = true
  try {
    processes.value = (await TerminalService.ProcessList(props.sessionId)) ?? []
    // 选中行若已消失则清空（排序由 filtered 统一处理）
    if (selectedRow.value && !processes.value.some((p) => p.pid === selectedRow.value!.pid)) {
      selectedRow.value = null
    }
  } catch (e: any) {
    ElMessage.error(`获取进程列表失败：${e?.message || e}`)
  } finally {
    loading.value = false
  }
}

function select(row: ProcessInfo) {
  selectedRow.value = row
}

async function kill() {
  const p = selectedRow.value
  if (!p) return
  const ok = await showConfirmDialog(
    '终止进程',
    `确定强制终止进程 ${p.pid}（${p.command || '未知'}）？`,
    true,
    '终止',
  )
  if (!ok) return
  try {
    await TerminalService.KillProcess(props.sessionId, p.pid)
    ElMessage.success(`已终止进程 ${p.pid}`)
    selectedRow.value = null
    await refresh()
  } catch (e: any) {
    ElMessage.error(`终止失败：${e?.message || e}`)
  }
}

// 激活时按设置的间隔自动刷新
function syncTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
  if (props.active && props.sessionId && auto.value) {
    refresh()
    timer = setInterval(refresh, settings.processRefreshInterval * 1000)
  }
}

watch(() => props.active, syncTimer)
watch(() => props.sessionId, syncTimer)
watch(auto, syncTimer)
watch(() => settings.processRefreshInterval, syncTimer)

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.pm-view {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  height: 100%;
  min-height: 0;
}

.pm-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.pm-search {
  flex: 1;
}

.pm-table {
  flex: 1;
  min-height: 0;
}

.pm-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}

.pm-info {
  font-size: 11.5px;
  color: var(--text-secondary);
}

.hot {
  color: #f56c6c;
  font-weight: 600;
}

.mono {
  font-family: var(--term-font);
}

.dim {
  color: var(--text-secondary);
}
</style>
