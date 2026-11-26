<template>
    <el-dialog :model-value="modelValue" title="压力测试" width="700px" destroy-on-close @update:model-value="onVisibleChange"
        @close="emit('stop')">
        <el-form label-width="90px">
            <el-form-item label="并发数">
                <el-input-number v-model="form.concurrency" :min="1" :max="1000" :step="1"
                    :disabled="running" />
            </el-form-item>
            <el-form-item label="总请求数">
                <el-input-number v-model="form.total" :min="1" :max="100000" :step="10" :disabled="running" />
            </el-form-item>
            <el-form-item label="超时(秒)">
                <el-input-number v-model="form.timeout" :min="1" :max="120" :step="1" :disabled="running" />
            </el-form-item>
        </el-form>

        <div class="stress-progress" v-if="running">
            <el-progress :percentage="percentage" :text-inside="true" :stroke-width="20" />
            <p class="stress-progress-text">{{ progress.current }} / {{ progress.total }}</p>
        </div>

        <div class="stress-result" v-if="result">
            <div class="stress-summary">
                <el-tag :type="result.failure === 0 ? 'success' : 'warning'" size="large">
                    {{ result.failure === 0 ? '全部通过' : result.success + ' 成功 / ' + result.failure + ' 失败' }}
                </el-tag>
                <span class="stress-qps">QPS: {{ result.qps }}</span>
                <span class="stress-dur">{{ result.duration }} ms</span>
            </div>

            <el-table :data="latencyRows" size="small" class="stress-table">
                <el-table-column prop="label" label="指标" width="80" />
                <el-table-column prop="min" label="最小(ms)" width="90" />
                <el-table-column prop="avg" label="平均(ms)" width="90" />
                <el-table-column prop="max" label="最大(ms)" width="90" />
                <el-table-column prop="p50" label="P50(ms)" width="90" />
                <el-table-column prop="p95" label="P95(ms)" width="90" />
                <el-table-column prop="p99" label="P99(ms)" width="90" />
            </el-table>

            <el-table :data="statusRows" size="small" class="stress-table" v-if="statusRows.length">
                <el-table-column prop="code" label="状态码" width="120" />
                <el-table-column prop="count" label="次数" />
                <el-table-column prop="pct" label="占比" width="100">
                    <template #default="{ row }">
                        <el-progress :percentage="row.pctNum" :stroke-width="10" />
                    </template>
                </el-table-column>
            </el-table>
        </div>

        <div class="stress-empty" v-if="!running && !result">
            <p>使用当前请求参数发起并发压力测试</p>
            <p class="stress-empty-hint">URL: {{ url }}</p>
        </div>

        <template #footer>
            <el-button @click="emit('update:modelValue', false)" :disabled="running">关闭</el-button>
            <el-button type="warning" @click="start" :loading="running" :disabled="!url">
                {{ running ? '测试中...' : '开始压测' }}
            </el-button>
        </template>
    </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive } from 'vue'
import type { StressTestResult } from '../../../bindings/spark/app/service/types/models.js'

interface StressForm {
    concurrency: number
    total: number
    timeout: number
}

const props = defineProps<{
    modelValue: boolean
    running: boolean
    progress: { current: number; total: number }
    result: StressTestResult | null
    latencyRows: { label: string; min: number; avg: number; max: number; p50: number; p95: number; p99: number }[]
    statusRows: { code: string; count: number; pct: string; pctNum: number }[]
    url: string
}>()

const emit = defineEmits<{
    (e: 'update:modelValue', v: boolean): void
    (e: 'start', form: StressForm): void
    (e: 'stop'): void
}>()

const form = reactive<StressForm>({ concurrency: 10, total: 100, timeout: 10 })

const percentage = computed(() =>
    props.progress.total > 0 ? Math.round((props.progress.current / props.progress.total) * 100) : 0,
)

function onVisibleChange(v: boolean) {
    emit('update:modelValue', v)
}

function start() {
    emit('start', { ...form })
}
</script>

<style scoped>
.stress-progress {
    padding: 16px 0;
    text-align: center;
}

.stress-progress-text {
    margin-top: 8px;
    font-size: 13px;
    color: var(--text-secondary);
}

.stress-result {
    margin-top: 16px;
}

.stress-summary {
    display: flex;
    align-items: center;
    gap: 16px;
    margin-bottom: 16px;
}

.stress-qps {
    font-weight: 700;
    font-size: 15px;
    color: var(--primary-color);
}

.stress-dur {
    font-size: 12px;
    color: var(--text-secondary);
}

.stress-table {
    margin-top: 12px;
}

.stress-empty {
    text-align: center;
    padding: 24px 0;
    color: var(--text-secondary);
    font-size: 14px;
}

.stress-empty-hint {
    font-size: 12px;
    margin-top: 4px;
    color: var(--text-tertiary);
    word-break: break-all;
}
</style>