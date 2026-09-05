<template>
    <div class="at-chat">
        <div ref="scrollRef" class="at-body">
            <div v-if="!entries.length" class="at-empty">
                直接说目标，如「查看 nginx 是否在运行及其内存占用」；AI 会直接回答或按授权执行命令
            </div>
            <template v-for="(e, i) in entries" :key="i">
                <div v-if="e.kind === 'user'" class="bubble user">{{ e.text }}</div>
                <div v-else-if="e.kind === 'reply'" class="bubble ai">{{ e.text }}</div>
                <div v-else-if="e.kind === 'notice'" class="notice" :class="{ err: e.error }">{{ e.text }}</div>
                <div v-else-if="e.kind === 'exec'" class="exec">
                    <div class="exec-head">
                        <span class="exec-cmd mono">$ {{ e.command }}</span>
                        <span class="exec-status" :class="statusClass(e)">{{ statusText(e) }}</span>
                    </div>
                    <div v-if="e.reason" class="exec-reason">{{ e.reason }}</div>
                    <div v-if="e.why" class="exec-why">{{ e.why }}</div>
                    <pre v-if="e.output" class="exec-output mono">{{ e.output }}</pre>
                </div>
            </template>
        </div>

        <div v-if="pending" class="at-ask">
            <div class="at-ask-title">
                是否执行？<span v-if="pending.ask.why" class="exec-why">{{ pending.ask.why }}</span>
            </div>
            <div v-if="pending.ask.reason" class="exec-reason">{{ pending.ask.reason }}</div>
            <el-input v-model="pending.edited" type="textarea" :autosize="{ minRows: 2, maxRows: 5 }"
                class="at-ask-input" />
            <div class="at-ask-actions">
                <el-button size="small" type="primary" @click="approve">批准</el-button>
                <el-button size="small" type="danger" @click="reject">拒绝</el-button>
            </div>
        </div>

        <div class="at-input">
            <el-input v-model="input" type="textarea" :autosize="{ minRows: 1, maxRows: 5 }" resize="none"
                :placeholder="sessionId ? '输入消息，Enter 发送，Shift+Enter 换行' : '请先连接 SSH 会话'" :disabled="!sessionId"
                @keydown.enter.exact.prevent="send" />
            <el-button v-if="!running" type="primary" :disabled="!canSend" @click="send">发送</el-button>
            <el-button v-else type="danger" @click="stop">停止</el-button>
        </div>

        <div class="at-bottom">
            <el-select v-model="model" size="small" class="at-model" filterable allow-create default-first-option
                title="模型" @change="onModelChange">
                <el-option v-for="m in modelOptions" :key="m" :label="m" :value="m" />
            </el-select>
            <el-select v-model="authMode" size="small" class="at-auth" :disabled="running" title="授权模式">
                <el-option label="仅可查看" value="ask" />
                <el-option label="敏感操作询问" value="sensitive" />
                <el-option label="完全授权" value="full" />
            </el-select>
            <div class="at-bottom-actions">
                <el-button size="small" text :disabled="!entries.length" @click="clear">清空</el-button>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Events } from '@wailsio/runtime'
import { ElMessage } from 'element-plus'
import { AgentService, AIService, EVENTS, type AgentAsk, type AgentDone, type AgentOutput, type AgentReply, type AgentStep } from '../utils/wails'
import { showConfirmDialog } from '../utils/dialog'
import { useSettingsStore } from '../stores/settings'

const props = defineProps<{ sessionId: string }>()

const settings = useSettingsStore()

type ExecEntry = {
    kind: 'exec'
    step: number
    command: string
    reason?: string
    why?: string
    status: 'pending' | 'running' | 'done' | 'rejected'
    output: string
    exitCode: number | null
}
type Entry =
    | { kind: 'user'; text: string }
    | { kind: 'reply'; text: string }
    | { kind: 'notice'; text: string; error?: boolean }
    | ExecEntry

const authMode = ref<'ask' | 'sensitive' | 'full'>('sensitive')
const model = ref('')
// 模型列表从供应商 /models 接口动态获取（不内置）
const modelList = ref<string[]>([])
const modelOptions = computed<string[]>(() => {
    const list = [...modelList.value]
    if (model.value && !list.includes(model.value)) list.unshift(model.value)
    return list
})
const input = ref('')
const histories = reactive<Record<string, Entry[]>>({})
const runningBySession = reactive<Record<string, boolean>>({})
const pendingBySession = reactive<Record<string, { ask: AgentAsk; edited: string } | null>>({})
const scrollRef = ref<HTMLElement | null>(null)
const unsubs: Array<() => void> = []

const entries = computed<Entry[]>(() => histories[props.sessionId] ?? [])
const running = computed(() => !!runningBySession[props.sessionId])
const pending = computed(() => pendingBySession[props.sessionId] ?? null)
const canSend = computed(() => input.value.trim().length > 0 && !running.value && !!props.sessionId)

function ensure(sid: string): Entry[] {
    if (!histories[sid]) histories[sid] = []
    return histories[sid]
}

function push(sid: string, e: Entry) {
    ensure(sid).push(e)
    scrollToBottom()
}

function scrollToBottom() {
    void nextTick(() => {
        if (scrollRef.value) scrollRef.value.scrollTop = scrollRef.value.scrollHeight
    })
}

function onModelChange() {
    if (model.value) settings.set('ai.model', model.value).catch(() => undefined)
}

function lastExec(sid: string): ExecEntry | null {
    const list = histories[sid] ?? []
    for (let i = list.length - 1; i >= 0; i--) {
        if (list[i].kind === 'exec') return list[i] as ExecEntry
    }
    return null
}

function statusText(e: ExecEntry): string {
    switch (e.status) {
        case 'pending':
            return '待确认'
        case 'running':
            return '执行中'
        case 'rejected':
            return '已拒绝'
        case 'done':
            return e.exitCode === null ? '完成' : `exit=${e.exitCode}`
    }
}

function statusClass(e: ExecEntry): string {
    if (e.status === 'rejected') return 'bad'
    if (e.status === 'done' && e.exitCode !== 0) return 'bad'
    if (e.status === 'pending') return 'warn'
    return ''
}

async function send() {
    const text = input.value.trim()
    if (!text || running.value) return
    if (!props.sessionId) {
        ElMessage.warning('请先连接 SSH 会话')
        return
    }
    push(props.sessionId, { kind: 'user', text })
    input.value = ''
    runningBySession[props.sessionId] = true
    try {
        await AgentService.Send(props.sessionId, text, authMode.value)
    } catch (e: any) {
        runningBySession[props.sessionId] = false
        push(props.sessionId, { kind: 'notice', text: `⚠️ ${e?.message || e}`, error: true })
    }
}

async function stop() {
    if (props.sessionId) await AgentService.Cancel(props.sessionId).catch(() => undefined)
}

async function approve() {
    const p = pendingBySession[props.sessionId]
    if (!p) return
    const cmd = p.edited.trim()
    if (!cmd) {
        ElMessage.warning('命令不能为空')
        return
    }
    pendingBySession[props.sessionId] = null
    await AgentService.Respond(props.sessionId, true, cmd).catch((e: any) => ElMessage.error(e?.message || String(e)))
}

async function reject() {
    pendingBySession[props.sessionId] = null
    await AgentService.Respond(props.sessionId, false, '').catch(() => undefined)
}

async function clear() {
    const ok = await showConfirmDialog('清空对话', '确定清空当前会话的 AI 对话历史？', true, '清空')
    if (!ok) return
    histories[props.sessionId] = []
    runningBySession[props.sessionId] = false
    pendingBySession[props.sessionId] = null
    await AgentService.Clear(props.sessionId).catch(() => undefined)
}

function onReply(evt: any) {
    const d = evt?.data as AgentReply | undefined
    if (!d?.sessionId) return
    if (d.done) {
        if (d.error) {
            const list = ensure(d.sessionId)
            const last = list[list.length - 1]
            if (last && last.kind === 'reply') {
                last.text = last.text ? `${last.text}\n\n⚠️ ${d.error}` : `⚠️ ${d.error}`
            } else {
                push(d.sessionId, { kind: 'notice', text: `⚠️ ${d.error}`, error: true })
            }
        }
        return
    }
    if (d.content) {
        const list = ensure(d.sessionId)
        const last = list[list.length - 1]
        if (last && last.kind === 'reply') {
            last.text += d.content
        } else {
            push(d.sessionId, { kind: 'reply', text: d.content })
        }
        scrollToBottom()
    }
}

function onStep(evt: any) {
    const d = evt?.data as AgentStep | undefined
    if (!d?.sessionId) return
    if (d.status === 'propose') {
        push(d.sessionId, {
            kind: 'exec',
            step: d.step,
            command: d.command || '',
            reason: d.reason,
            why: d.why,
            status: 'pending',
            output: '',
            exitCode: null,
        })
    } else if (d.status === 'running') {
        const e = lastExec(d.sessionId)
        if (e) {
            e.status = 'running'
            e.command = d.command || e.command
        }
    } else if (d.status === 'rejected') {
        const e = lastExec(d.sessionId)
        if (e) e.status = 'rejected'
    }
}

function onAsk(evt: any) {
    const d = evt?.data as AgentAsk | undefined
    if (!d?.sessionId) return
    pendingBySession[d.sessionId] = { ask: d, edited: d.command }
    scrollToBottom()
}

function onOutput(evt: any) {
    const d = evt?.data as AgentOutput | undefined
    if (!d?.sessionId) return
    const e = lastExec(d.sessionId)
    if (e) {
        e.command = d.command
        e.output = d.output
        e.exitCode = d.exitCode
        e.status = 'done'
    }
    scrollToBottom()
}

function onDone(evt: any) {
    const d = evt?.data as AgentDone | undefined
    if (!d?.sessionId) return
    runningBySession[d.sessionId] = false
    pendingBySession[d.sessionId] = null
    if (d.error) {
        push(d.sessionId, { kind: 'notice', text: `⚠️ ${d.error}`, error: true })
    } else if (d.summary && d.summary !== '已取消' && d.summary !== '已由用户取消') {
        push(d.sessionId, { kind: 'notice', text: d.summary })
    }
}

onMounted(() => {
    AIService.GetConfig()
        .then((cfg) => { model.value = cfg.model })
        .catch(() => undefined)
    AIService.ListModels()
        .then((ids) => {
            if (ids && ids.length) modelList.value = ids
        })
        .catch(() => undefined)
    unsubs.push(Events.On(EVENTS.agentReply, onReply))
    unsubs.push(Events.On(EVENTS.agentStep, onStep))
    unsubs.push(Events.On(EVENTS.agentAsk, onAsk))
    unsubs.push(Events.On(EVENTS.agentOutput, onOutput))
    unsubs.push(Events.On(EVENTS.agentDone, onDone))
})

onBeforeUnmount(() => {
    unsubs.forEach((u) => u())
})
</script>

<style scoped>
.at-chat {
    display: flex;
    flex-direction: column;
    height: 100%;
    gap: 8px;
    padding: 10px;
}

.at-bottom {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
}

.at-bottom .el-select {
    max-width: 50%;
}

.at-model {
    flex: 1;
    min-width: 0;
}

.at-auth {
    flex-shrink: 0;
}

.at-bottom-actions {
    margin-left: auto;
}

.at-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.at-empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 0 16px;
    font-size: 12px;
    color: var(--text-muted);
}

.bubble {
    max-width: 88%;
    padding: 8px 10px;
    border-radius: 10px;
    font-size: 12.5px;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-word;
}

.bubble.user {
    align-self: flex-end;
    background: var(--active-bg);
    color: var(--active-text);
}

.bubble.ai {
    align-self: flex-start;
    background: var(--hover-bg);
    color: var(--text-primary);
}

.notice {
    align-self: center;
    font-size: 11.5px;
    color: var(--text-muted);
    text-align: center;
}

.notice.err {
    color: #f56c6c;
}

.exec {
    border: 1px solid var(--border-color);
    background: var(--term-bg);
    border-radius: 6px;
    padding: 6px 8px;
}

.exec-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
}

.exec-cmd {
    font-size: 12px;
    color: var(--text-primary);
    word-break: break-all;
    white-space: pre-wrap;
}

.exec-status {
    flex-shrink: 0;
    font-size: 11px;
    color: #34c759;
}

.exec-status.warn {
    color: #e6a23c;
}

.exec-status.bad {
    color: #f56c6c;
}

.exec-reason {
    font-size: 11.5px;
    color: var(--text-secondary);
    margin-top: 3px;
}

.exec-why {
    font-size: 11.5px;
    color: #e6a23c;
    margin-top: 3px;
}

.exec-output {
    margin: 6px 0 0;
    font-size: 11.5px;
    line-height: 1.5;
    color: var(--text-primary);
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 160px;
    overflow-y: auto;
    background: var(--hover-bg);
    border-radius: 4px;
    padding: 5px 7px;
}

.at-ask {
    flex-shrink: 0;
    border: 1px solid #e6a23c;
    background: rgba(230, 162, 60, 0.08);
    border-radius: 6px;
    padding: 8px 9px;
    display: flex;
    flex-direction: column;
    gap: 6px;
}

.at-ask-title {
    font-size: 12.5px;
    font-weight: 600;
    color: #e6a23c;
}

.at-ask-input :deep(.el-textarea__inner) {
    font-size: 12px;
    font-family: var(--term-font);
    line-height: 1.5;
}

.at-ask-actions {
    display: flex;
    gap: 6px;
}

.at-input {
    flex-shrink: 0;
    display: flex;
    align-items: flex-end;
    gap: 6px;
}

.at-input :deep(.el-textarea__inner) {
    font-size: 12.5px;
    line-height: 1.5;
}
</style>
