<template>
    <el-dialog :model-value="modelValue" title="批量执行命令" width="720px" :close-on-click-modal="false"
        @update:model-value="(v: boolean) => emit('update:modelValue', v)">
        <div class="be-targets">
            目标服务器（{{ targets.length }}）：
            <el-tag v-for="t in targets" :key="t.id" size="small" type="info" class="be-target-tag">
                {{ t.name }}
            </el-tag>
        </div>

        <el-form label-width="80px" label-position="left">
            <el-form-item label="命令">
                <el-input v-model="command" type="textarea" :rows="4" placeholder="例如：uptime && df -h"
                    class="mono" />
            </el-form-item>
            <el-form-item label="超时">
                <el-input-number v-model="timeoutSec" :min="5" :max="600" :step="5" controls-position="right" />
                <span class="be-unit">秒</span>
            </el-form-item>
        </el-form>

        <div v-if="results.length" class="be-results">
            <div class="be-summary">
                完成 {{ doneCount }} / {{ results.length }} ·
                成功 <b class="ok">{{ successCount }}</b> ·
                失败 <b class="err">{{ results.length - successCount }}</b>
            </div>
            <el-collapse v-model="activeNames">
                <el-collapse-item v-for="r in results" :key="r.connId" :name="String(r.connId)">
                    <template #title>
                        <span class="be-title">
                            <el-tag size="small" :type="r.error || r.exitCode !== 0 ? 'danger' : 'success'"
                                effect="plain">
                                {{ r.error ? '失败' : r.exitCode !== 0 ? `退出码 ${r.exitCode}` : '成功' }}
                            </el-tag>
                            <b class="be-name">{{ r.name }}</b>
                            <span class="mono dim">{{ r.host }}</span>
                            <span class="be-dur">{{ r.duration }}ms</span>
                        </span>
                    </template>
                    <div v-if="r.error" class="be-error">{{ r.error }}</div>
                    <pre v-if="r.output" class="be-output mono">{{ r.output }}</pre>
                </el-collapse-item>
            </el-collapse>
        </div>

        <template #footer>
            <el-button @click="emit('update:modelValue', false)">关闭</el-button>
            <el-button type="primary" :loading="running" :disabled="!command.trim()" @click="run">
                {{ results.length ? '重新执行' : '执行' }}
            </el-button>
        </template>
    </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { BatchService, type BatchExecResult, type SavedConnection } from '../utils/wails'

const props = defineProps<{
    modelValue: boolean
    targets: SavedConnection[]
}>()

const emit = defineEmits<{
    (e: 'update:modelValue', v: boolean): void
}>()

const command = ref('')
const timeoutSec = ref(60)
const running = ref(false)
const results = ref<BatchExecResult[]>([])
const activeNames = ref<string[]>([])

const doneCount = computed(() => results.value.length)
const successCount = computed(
    () => results.value.filter((r) => !r.error && r.exitCode === 0).length,
)

async function run() {
    const cmd = command.value.trim()
    if (!cmd) return
    if (!props.targets.length) {
        ElMessage.warning('没有可执行的服务器')
        return
    }
    running.value = true
    results.value = []
    try {
        const ids = props.targets.map((t) => t.id!)
        const list = (await BatchService.Execute(ids, cmd, timeoutSec.value)) ?? []
        results.value = list
        // 默认展开失败项，便于排查
        activeNames.value = list
            .filter((r) => r.error || r.exitCode !== 0)
            .map((r) => String(r.connId))
        const failed = list.length - successCount.value
        if (failed > 0) {
            ElMessage.warning(`执行完成：${list.length - failed} 成功，${failed} 失败`)
        } else {
            ElMessage.success(`执行完成：${list.length} 台服务器全部成功`)
        }
    } catch (e: any) {
        ElMessage.error(`执行失败：${e?.message || e}`)
    } finally {
        running.value = false
    }
}
</script>

<style scoped>
.be-targets {
    font-size: 12.5px;
    color: var(--text-secondary);
    margin-bottom: 12px;
    line-height: 2;
}

.be-target-tag {
    margin-left: 6px;
}

.be-unit {
    margin-left: 8px;
    font-size: 12px;
    color: var(--text-secondary);
}

.mono {
    font-family: Cascadia Code, JetBrains Mono, Consolas, monospace;
}

.dim {
    color: var(--text-muted);
}

.be-results {
    margin-top: 4px;
    border-top: 1px solid var(--border-color);
    padding-top: 8px;
}

.be-summary {
    font-size: 12.5px;
    color: var(--text-secondary);
    margin-bottom: 8px;
}

.be-summary .ok {
    color: var(--success-color);
}

.be-summary .err {
    color: var(--danger-color);
}

.be-title {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}

.be-name {
    font-size: 13px;
    flex-shrink: 0;
}

.be-dur {
    margin-left: auto;
    margin-right: 10px;
    font-size: 12px;
    color: var(--text-muted);
}

.be-error {
    font-size: 12.5px;
    color: var(--danger-color);
    margin-bottom: 6px;
    word-break: break-all;
}

.be-output {
    margin: 0;
    max-height: 260px;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-all;
    font-size: 12px;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    padding: 8px 10px;
    color: var(--text-primary);
}
</style>