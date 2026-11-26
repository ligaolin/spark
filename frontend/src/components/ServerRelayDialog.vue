<template>
    <el-dialog :model-value="modelValue" title="传输到其他服务器（服务器间直传）" width="620px"
        :close-on-click-modal="false" @update:model-value="(v: boolean) => emit('update:modelValue', v)"
        @open="onOpen">
        <div class="sr-src">
            源：<span class="mono">{{ srcName }}</span>
            <el-tag v-if="isDir" size="small" type="info" class="sr-tag">目录（含全部内容）</el-tag>
        </div>

        <el-form label-width="90px" label-position="left" size="small" :disabled="running">
            <el-form-item label="目标服务器">
                <el-select v-model="targetId" filterable placeholder="选择已保存的 SSH 连接" style="width: 100%">
                    <el-option v-for="c in targets" :key="c.id" :label="`${c.name}（${c.username}@${c.host}:${c.port}）`"
                        :value="c.id!" />
                </el-select>
            </el-form-item>
            <el-form-item label="目标目录">
                <el-input v-model="destDir" placeholder="绝对路径，例如 /data/app" class="mono" />
            </el-form-item>
        </el-form>

        <div v-if="running" class="sr-progress">
            <div class="sr-cur mono">{{ progress.name || '准备中…' }}</div>
            <el-progress :percentage="percent" :stroke-width="10" />
        </div>
        <div class="sr-hint">数据通过本应用内存直接转发到目标服务器，不经过本地磁盘。目标端同名文件会被覆盖。</div>

        <template #footer>
            <el-button @click="emit('update:modelValue', false)" :disabled="running">关闭</el-button>
            <el-button type="primary" :loading="running" :disabled="!targetId || !destDir.trim()" @click="run">
                开始传输
            </el-button>
        </template>
    </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Events } from '@wailsio/runtime'
import { useConnectionsStore } from '../stores/connections'
import {
    EVENTS,
    makeConnectOptions,
} from '../utils/wails'
import type { ConnectOptions, SavedConnection } from '../utils/wails'
import type { FileBackend } from '../utils/fileBackend'
import { resolveHostKeyIssue } from '../utils/hostkey'

const props = defineProps<{
    modelValue: boolean
    srcPath: string
    srcName: string
    isDir: boolean
    backend: FileBackend
    sessionId?: string
    // 排除的连接 id（源服务器自身）
    excludeConnId?: number
}>()

const emit = defineEmits<{
    (e: 'update:modelValue', v: boolean): void
    (e: 'done'): void
}>()

const connStore = useConnectionsStore()
const targetId = ref<number>()
const destDir = ref('/')
const running = ref(false)
const progress = ref({ name: '', done: 0, total: 0 })

const targets = computed(() =>
    connStore.list.filter((c) => c.type !== 'ftp' && c.id !== props.excludeConnId),
)
const percent = computed(() =>
    progress.value.total > 0 ? Math.min(100, Math.round((progress.value.done / progress.value.total) * 100)) : 0,
)

let unProgress: (() => void) | null = null

function watchProgress(on: boolean) {
    if (on && !unProgress) {
        unProgress = Events.On(EVENTS.transferProgress, (evt: any) => {
            const p = evt.data
            if (!p || p.op !== 'copy' || p.sessionId !== props.sessionId) return
            progress.value = { name: p.name || '', done: p.done, total: p.total }
        })
    } else if (!on && unProgress) {
        unProgress()
        unProgress = null
    }
}

function onOpen() {
    if (!connStore.list.length) connStore.load()
    targetId.value = undefined
    destDir.value = '/'
    progress.value = { name: '', done: 0, total: 0 }
}

function optsFromConn(c: SavedConnection): ConnectOptions {
    return makeConnectOptions({
        host: c.host,
        port: c.port,
        username: c.username,
        password: c.password,
        useKey: c.useKey,
        privateKey: c.privateKey,
        passphrase: c.passphrase,
        forwardAgent: c.forwardAgent,
        jumpId: c.jumpId ?? 0,
    })
}

async function run(retry = 0) {
    const target = targets.value.find((c) => c.id === targetId.value)
    if (!target) {
        ElMessage.warning('请选择目标服务器')
        return
    }
    if (!destDir.value.trim().startsWith('/')) {
        ElMessage.warning('目标目录必须是绝对路径')
        return
    }
    if (!props.backend.relayTo) {
        ElMessage.error('当前后端不支持服务器间直传')
        return
    }
    running.value = true
    progress.value = { name: '', done: 0, total: 0 }
    watchProgress(true)
    try {
        const targetPath = await props.backend.relayTo(props.srcPath, target.id!, destDir.value.trim())
        ElMessage.success(`已传输到 ${target.name}：${targetPath}`)
        emit('done')
        emit('update:modelValue', false)
    } catch (e: any) {
        // 目标服务器主机密钥未信任：询问后保存并重试一次
        if (retry === 0) {
            const accepted = await resolveHostKeyIssue(e, optsFromConn(target))
            if (accepted) {
                watchProgress(false)
                running.value = false
                await run(1)
                return
            }
        }
        ElMessage.error(`传输失败：${e?.message || e}`)
    } finally {
        watchProgress(false)
        running.value = false
    }
}
</script>

<style scoped>
.sr-src {
    font-size: 12.5px;
    color: var(--text-secondary);
    margin-bottom: 12px;
}

.sr-tag {
    margin-left: 8px;
}

.sr-progress {
    margin: 4px 0 10px;
}

.sr-cur {
    font-size: 12px;
    color: var(--text-muted);
    margin-bottom: 6px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.sr-hint {
    font-size: 12px;
    color: var(--text-muted);
    line-height: 1.6;
}

.mono {
    font-family: Cascadia Code, JetBrains Mono, Consolas, monospace;
}
</style>