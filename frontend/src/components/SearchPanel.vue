<template>
    <div class="search-panel">
        <div class="search-bar">
            <el-radio-group v-model="mode" size="small">
                <el-radio-button value="name">文件名</el-radio-button>
                <el-radio-button value="content">文件内容</el-radio-button>
            </el-radio-group>
            <el-button size="small" type="primary" :loading="searching" @click="doSearch">搜索</el-button>
        </div>

        <div class="search-bar">
            <el-input ref="keywordRef" v-model="keyword" size="small" placeholder="搜索" clearable
                @keyup.enter="doSearch" />
            <el-button size="small" text :class="{ active: caseSensitive }" title="区分大小写"
                @click="caseSensitive = !caseSensitive">Aa</el-button>
            <el-button size="small" text :class="{ active: useRegex }" :disabled="mode === 'name'"
                title="使用正则表达式（仅内容搜索）" @click="useRegex = !useRegex">.*</el-button>
        </div>

        <div v-if="mode === 'content'" class="search-bar replace-bar">
            <el-input v-model="replacement" size="small" placeholder="替换为（$1 引用分组）" clearable
                @keyup.enter="doReplaceAll" />
            <el-button size="small" type="warning" :loading="replacing" @click="doReplaceAll">
                全部替换
            </el-button>
        </div>

        <div class="search-bar exclude-bar">
            <span class="exclude-label">排除</span>
            <el-input v-model="exclude" size="small" placeholder="glob，逗号/换行分隔，如 node_modules, *.log" clearable
                @keyup.enter="doSearch" />
        </div>

        <div class="search-root" :title="root">从 {{ root }} 递归搜索</div>

        <div class="search-results">
            <el-table :data="results" size="small" height="100%" empty-text="输入关键字后点击「搜索」" @row-dblclick="onPick">
                <el-table-column label="名称" min-width="150">
                    <template #default="{ row }">
                        <el-icon v-if="row.isDir" color="#e6c06c">
                            <Folder />
                        </el-icon>
                        <el-icon v-else color="var(--text-secondary)">
                            <Document />
                        </el-icon>
                        <span class="res-name" :title="row.name">{{ row.name }}</span>
                    </template>
                </el-table-column>
                <el-table-column label="路径" prop="path" min-width="180" show-overflow-tooltip />
                <el-table-column v-if="mode === 'content'" label="行" width="48" align="right">
                    <template #default="{ row }"><span class="dim">{{ row.lineNo }}</span></template>
                </el-table-column>
                <el-table-column v-if="mode === 'content'" label="匹配内容" min-width="200" show-overflow-tooltip>
                    <template #default="{ row }"><span class="dim">{{ row.line }}</span></template>
                </el-table-column>
                <el-table-column label="大小" width="90" align="right">
                    <template #default="{ row }">
                        <span class="dim">{{ row.isDir ? '-' : formatSize(row.size) }}</span>
                    </template>
                </el-table-column>
            </el-table>
        </div>

        <div class="search-status">
            <span>{{ statusText }}</span>
            <span v-if="mode === 'content'" class="hint">内容搜索会跳过二进制文件及超过 10MB 的文件</span>
        </div>
    </div>
</template>

<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Folder, Document } from '@element-plus/icons-vue'
import { formatSize } from '../types'
import type { SearchResult, SearchOptions } from '../types'
import type { FileBackend } from '../utils/fileBackend'
import { showConfirmDialog } from '../utils/dialog'
import { useSettingsStore } from '../stores/settings'

const props = defineProps<{ backend: FileBackend }>()

const settings = useSettingsStore()

const emit = defineEmits<{
    (e: 'pick', result: SearchResult): void
}>()

const keyword = ref('')
const mode = ref<'name' | 'content'>('name')
const caseSensitive = ref(false)
const useRegex = ref(false)
const replacement = ref('')
const exclude = ref('')
const searching = ref(false)
const replacing = ref(false)
const results = ref<SearchResult[]>([])
const root = ref('')
const statusText = ref('')
const keywordRef = ref()

function options(): SearchOptions {
    // 实际排除 = 设置里的全局排除 + 搜索面板里填写的排除（两者叠加）
    const configured = (settings.searchExclude || '').trim()
    const input = exclude.value.trim()
    const combined = [configured, input].filter(Boolean).join('\n')
    return {
        caseSensitive: caseSensitive.value,
        useRegex: useRegex.value,
        exclude: combined,
    }
}

// open 切换到指定目录。切换搜索面板时保留搜索条件与结果；仅当搜索范围
// 目录发生变化时才清空旧结果（旧结果对新目录不再有效）。
function open(rootDir: string) {
    if (root.value !== rootDir) {
        root.value = rootDir
        results.value = []
        statusText.value = ''
    }
    nextTick(() => keywordRef.value?.focus())
}

async function doSearch() {
    const kw = keyword.value.trim()
    if (!kw) {
        ElMessage.warning('请输入搜索关键字')
        return
    }
    if (!root.value) {
        ElMessage.warning('请先进入目录')
        return
    }
    searching.value = true
    statusText.value = ''
    try {
        results.value = await props.backend.search(root.value, kw, mode.value, options())
        statusText.value =
            results.value.length > 0 ? `共 ${results.value.length} 条结果` : '没有匹配结果'
    } catch (e: any) {
        ElMessage.error(`搜索失败：${e?.message || e}`)
        statusText.value = ''
    } finally {
        searching.value = false
    }
}

async function doReplaceAll() {
    const kw = keyword.value.trim()
    if (!kw) {
        ElMessage.warning('请输入搜索关键字')
        return
    }
    if (!root.value) {
        ElMessage.warning('请先进入目录')
        return
    }
    const rep = replacement.value
    const ok = await showConfirmDialog(
        '替换',
        `确定将「${kw}」在搜索范围内的所有匹配替换为「${rep || '（空）'}」？此操作不可撤销。`,
        true,
        '全部替换',
    )
    if (!ok) return
    replacing.value = true
    try {
        const res = await props.backend.replace(root.value, kw, rep, mode.value, options())
        await doSearch() // 重新搜索，刷新结果列表
        statusText.value = `已替换 ${res.occurrences} 处（${res.files} 个文件）`
        ElMessage.success(statusText.value)
    } catch (e: any) {
        ElMessage.error(`替换失败：${e?.message || e}`)
    } finally {
        replacing.value = false
    }
}

function onPick(row: SearchResult) {
    emit('pick', row)
}

defineExpose({ open })
</script>

<style scoped>
.search-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 6px;
    padding: 6px;
}

.search-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
}

.search-bar :deep(.el-input) {
    flex: 1;
    min-width: 0;
}

.search-bar .el-button {
    font-family: var(--term-font);
    flex-shrink: 0;
}

.search-bar .el-button.active {
    color: var(--active-text);
    background: var(--hover-strong);
}

.replace-bar :deep(.el-input) {
    border-color: var(--accent);
}

.exclude-label {
    font-size: 12px;
    color: var(--text-secondary);
    flex-shrink: 0;
    user-select: none;
}

.search-root {
    font-family: var(--term-font);
    font-size: 11.5px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex-shrink: 0;
}

.search-results {
    flex: 1;
    min-height: 0;
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
    font-size: 12px;
    color: var(--text-secondary);
    min-height: 18px;
    flex-shrink: 0;
}

.hint {
    color: var(--text-secondary);
    opacity: 0.8;
    text-align: right;
}
</style>
