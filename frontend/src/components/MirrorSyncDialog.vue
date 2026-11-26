<template>
    <el-dialog :model-value="modelValue" title="镜像同步（本地 ↔ SFTP 目录）" width="760px" :close-on-click-modal="false"
        @update:model-value="(v: boolean) => emit('update:modelValue', v)" @open="onOpen">
        <el-form label-width="90px" label-position="left" size="small">
            <el-form-item label="同步方向">
                <el-radio-group v-model="direction" @change="resetDiff">
                    <el-radio-button value="upload">上传：本地 → 远程</el-radio-button>
                    <el-radio-button value="download">下载：远程 → 本地</el-radio-button>
                </el-radio-group>
            </el-form-item>
            <el-form-item label="本地目录">
                <div class="ms-row">
                    <el-input v-model="localDir" placeholder="选择本地目录" readonly />
                    <el-button @click="pickLocalDir">选择…</el-button>
                </div>
            </el-form-item>
            <el-form-item label="远程目录">
                <el-input v-model="remoteDir" placeholder="远程目录" class="mono" />
            </el-form-item>
            <el-form-item label="选项">
                <el-checkbox v-model="deleteExtra" @change="resetDiff">
                    删除目标端多余的文件（只删文件，不删目录）
                </el-checkbox>
            </el-form-item>
        </el-form>

        <div v-if="diff" class="ms-result">
            <div class="ms-summary">
                新增 <b>{{ newCount }}</b> · 更新 <b>{{ changedCount }}</b> · 多余 <b>{{ extraCount }}</b>
                <span v-if="transferCount" class="ms-bytes">待传输 {{ formatSize(transferBytes) }}</span>
            </div>
            <el-table v-if="items.length" ref="tableRef" :data="items" size="small" height="300"
                @selection-change="onSelectionChange">
                <el-table-column type="selection" width="36" />
                <el-table-column label="操作" width="110">
                    <template #default="{ row }">
                        <el-tag size="small" :type="row.action.startsWith('delete') ? 'danger' : 'primary'" effect="plain">
                            {{ actionLabel(row) }}
                        </el-tag>
                    </template>
                </el-table-column>
                <el-table-column label="相对路径" min-width="280">
                    <template #default="{ row }">
                        <span class="mono">{{ row.rel }}</span>
                    </template>
                </el-table-column>
                <el-table-column label="大小" width="100" align="right">
                    <template #default="{ row }">
                        <span class="dim">{{ row.action.startsWith('delete') ? '-' : formatSize(row.size) }}</span>
                    </template>
                </el-table-column>
            </el-table>
            <el-empty v-else description="两侧目录已一致，无需同步" :image-size="60" />
        </div>
        <div v-else class="ms-hint">选择本地目录后点击「比较差异」，先预览再执行（不会自动修改文件）。</div>

        <template #footer>
            <el-button @click="emit('update:modelValue', false)">关闭</el-button>
            <el-button :loading="comparing" :disabled="running" @click="compare">比较差异</el-button>
            <el-button type="primary" :loading="running" :disabled="!selected.length" @click="execute">
                执行同步{{ selected.length ? `（${selected.length} 项）` : '' }}
            </el-button>
        </template>
    </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { LocalService } from '../utils/wails'
import type { MirrorDiff, MirrorItem } from '../utils/wails'
import { formatSize } from '../types'
import { joinPath, type FileBackend } from '../utils/fileBackend'
import { runBatchTransfer } from '../utils/transferBatch'
import type { TransferKind } from '../stores/transfers'

const props = defineProps<{
    modelValue: boolean
    backend: FileBackend
    remoteDir: string
    sessionId?: string
    transferKind?: TransferKind
    transferScope?: string
}>()

const emit = defineEmits<{
    (e: 'update:modelValue', v: boolean): void
    // 同步完成后通知父组件刷新面板
    (e: 'done'): void
}>()

const direction = ref<'upload' | 'download'>('upload')
const localDir = ref('')
const remoteDir = ref('')
const deleteExtra = ref(false)
const diff = ref<MirrorDiff | null>(null)
const selected = ref<MirrorItem[]>([])
const comparing = ref(false)
const running = ref(false)
const tableRef = ref<any>()

const items = computed<MirrorItem[]>(() => diff.value?.items ?? [])
const newCount = computed(() => items.value.filter((i) => i.reason === 'new').length)
const changedCount = computed(() => items.value.filter((i) => i.reason === 'changed').length)
const extraCount = computed(() => items.value.filter((i) => i.reason === 'extra').length)
const transferCount = computed(() => items.value.length - extraCount.value)
const transferBytes = computed(() => diff.value?.bytes ?? 0)

function onOpen() {
    remoteDir.value = props.remoteDir
    diff.value = null
    selected.value = []
}

function resetDiff() {
    diff.value = null
    selected.value = []
}

async function pickLocalDir() {
    try {
        const dir = await LocalService.PickDirectory()
        if (dir) {
            localDir.value = dir
            resetDiff()
        }
    } catch (e: any) {
        ElMessage.error(`选择目录失败：${e?.message || e}`)
    }
}

async function compare() {
    if (!localDir.value || !remoteDir.value.trim()) {
        ElMessage.warning('请先填写本地目录和远程目录')
        return
    }
    if (!props.backend.compareMirror) {
        ElMessage.error('当前后端不支持镜像比较')
        return
    }
    comparing.value = true
    try {
        diff.value = await props.backend.compareMirror(
            localDir.value,
            remoteDir.value.trim(),
            direction.value,
            deleteExtra.value,
        )
        selected.value = [...items.value]
        await nextTick()
        if (items.value.length) tableRef.value?.toggleAllSelection?.()
    } catch (e: any) {
        diff.value = null
        ElMessage.error(`比较失败：${e?.message || e}`)
    } finally {
        comparing.value = false
    }
}

function joinLocal(base: string, rel: string): string {
    const sep = base.includes('\\') ? '\\' : '/'
    return `${base.replace(/[\\/]+$/, '')}${sep}${rel.replace(/\//g, sep)}`
}

function onSelectionChange(rows: MirrorItem[]) {
    selected.value = rows
}

function actionLabel(item: MirrorItem): string {
    switch (item.action) {
        case 'upload':
            return item.reason === 'new' ? '上传（新增）' : '上传（更新）'
        case 'download':
            return item.reason === 'new' ? '下载（新增）' : '下载（更新）'
        case 'delete-remote':
            return '删除远程（多余）'
        case 'delete-local':
            return '删除本地（多余）'
    }
    return item.action
}

async function execute() {
    const items = selected.value
    if (!items.length) return
    running.value = true
    try {
        const uploads = items.filter((i) => i.action === 'upload')
        const downloads = items.filter((i) => i.action === 'download')
        const deletes = items.filter((i) => i.action.startsWith('delete'))
        const base = {
            kind: props.transferKind ?? ('sftp' as TransferKind),
            scope: props.transferScope ?? 'mirror-sync',
            sessionId: props.sessionId ?? '',
        }
        if (uploads.length) {
            await runBatchTransfer({
                ...base,
                op: 'upload',
                label: `镜像同步上传 ${uploads.length} 项`,
                items: uploads.map((i) => ({
                    name: i.rel,
                    run: () =>
                        props.backend.upload(joinLocal(localDir.value, i.rel), joinPath(remoteDir.value.trim(), i.rel, '/')),
                })),
            })
        }
        if (downloads.length) {
            await runBatchTransfer({
                ...base,
                op: 'download',
                label: `镜像同步下载 ${downloads.length} 项`,
                items: downloads.map((i) => ({
                    name: i.rel,
                    run: () =>
                        props.backend.download(
                            joinPath(remoteDir.value.trim(), i.rel, '/'),
                            joinLocal(localDir.value, i.rel),
                            false,
                        ),
                })),
            })
        }
        if (deletes.length) {
            await runBatchTransfer({
                ...base,
                op: 'delete',
                label: `镜像同步删除 ${deletes.length} 项`,
                items: deletes.map((i) => ({
                    name: i.rel,
                    run: () =>
                        i.action === 'delete-local'
                            ? LocalService.Remove(joinLocal(localDir.value, i.rel))
                            : props.backend.remove(joinPath(remoteDir.value.trim(), i.rel, '/'), false),
                })),
            })
        }
        emit('done')
        // 同步后重新比较，显示剩余差异（通常为空）
        await compare()
    } catch (e: any) {
        ElMessage.error(`同步失败：${e?.message || e}`)
    } finally {
        running.value = false
    }
}
</script>

<style scoped>
.ms-row {
    display: flex;
    gap: 8px;
    width: 100%;
}

.ms-row .el-input {
    flex: 1;
}

.ms-result {
    border-top: 1px solid var(--border-color);
    padding-top: 8px;
}

.ms-summary {
    font-size: 12.5px;
    color: var(--text-secondary);
    margin-bottom: 8px;
}

.ms-summary b {
    color: var(--text-primary);
}

.ms-bytes {
    margin-left: 10px;
    color: var(--text-muted);
}

.ms-hint {
    font-size: 12.5px;
    color: var(--text-muted);
    padding: 8px 0;
}

.mono {
    font-family: Cascadia Code, JetBrains Mono, Consolas, monospace;
}

.dim {
    color: var(--text-muted);
}
</style>