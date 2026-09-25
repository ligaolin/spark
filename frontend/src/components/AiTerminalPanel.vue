<template>
    <div class="ai-chat">
        <!-- 顶部：标题 -->
        <div class="ai-head">
            <span class="ai-title">
                <el-icon>
                    <MagicStick />
                </el-icon>
                AI 助手
            </span>
        </div>

        <!-- 消息区 -->
        <div ref="scrollRef" class="ai-body" @scroll.passive="onScroll">
            <div v-if="!entries.length" class="ai-empty">
                <el-icon :size="30" color="var(--accent)">
                    <MagicStick />
                </el-icon>
                <div class="empty-title">直接说目标，AI 会回答或按授权在服务器上执行命令</div>
                <div class="chips">
                    <button v-for="s in suggestions" :key="s" class="chip" :disabled="!sessionId"
                        @click="sendSuggestion(s)">
                        {{ s }}
                    </button>
                </div>
            </div>

            <template v-for="(e, i) in entries" :key="i">
                <!-- 用户消息 -->
                <div v-if="e.kind === 'user'" class="row end">
                    <div class="bubble user">{{ e.text }}</div>
                </div>

                <!-- AI 消息：Markdown 渲染 -->
                <div v-else-if="e.kind === 'reply'" class="row">
                    <div class="avatar">
                        <el-icon>
                            <MagicStick />
                        </el-icon>
                    </div>
                    <div class="ai-msg">
                        <MdPreview v-if="e.text" :model-value="e.text" :theme="mdTheme" language="zh-CN"
                            :editor-id="`ai-md-${sessionId}-${i}`" class="md-body" />
                        <span v-if="e.streaming" class="caret" />
                        <div v-if="!e.streaming && e.text" class="msg-actions">
                            <button class="mini" title="复制" @click="copyText(e.text)">
                                <el-icon>
                                    <CopyDocument />
                                </el-icon>
                            </button>
                        </div>
                    </div>
                </div>

                <!-- 系统提示 -->
                <div v-else-if="e.kind === 'notice'" class="notice" :class="{ err: e.error }">{{ e.text }}</div>

                <!-- 命令卡片 -->
                <div v-else class="exec" :class="e.status">
                    <div class="exec-head" @click="e.collapsed = !e.collapsed">
                        <el-icon class="exec-caret">
                            <ArrowRight v-if="e.collapsed" />
                            <ArrowDown v-else />
                        </el-icon>
                        <span class="exec-cmd mono">{{ e.command }}</span>
                        <span class="exec-status" :class="statusClass(e)">{{ statusText(e) }}</span>
                    </div>
                    <div v-if="e.reason" class="exec-reason">{{ e.reason }}</div>
                    <div v-if="e.why" class="exec-why">{{ e.why }}</div>
                    <div v-show="!e.collapsed" class="exec-body">
                        <div class="exec-tools">
                            <button class="mini" title="复制命令" @click.stop="copyText(e.command)">
                                <el-icon>
                                    <CopyDocument />
                                </el-icon>
                            </button>
                            <button v-if="e.output" class="mini" title="复制输出" @click.stop="copyText(e.output)">
                                <el-icon>
                                    <DocumentCopy />
                                </el-icon>
                            </button>
                        </div>
                        <pre v-if="e.output" class="exec-output mono">{{ e.output }}</pre>
                        <div v-else-if="e.status === 'running'" class="exec-running">执行中…</div>
                    </div>
                </div>
            </template>

            <!-- 思考中 -->
            <div v-if="thinkingLabel" class="row">
                <div class="avatar">
                    <el-icon>
                        <MagicStick />
                    </el-icon>
                </div>
                <div class="thinking">
                    <span class="dots"><i></i><i></i><i></i></span>
                    {{ thinkingLabel }}
                </div>
            </div>
        </div>

        <!-- 回到底部 -->
        <button v-if="!stick && entries.length" class="to-bottom" title="回到最新" @click="scrollToLatest(true)">
            <el-icon>
                <ArrowDown />
            </el-icon>
        </button>

        <!-- 待确认命令 -->
        <div v-if="pending" class="ask">
            <div class="ask-title">
                <el-icon>
                    <WarnTriangleFilled />
                </el-icon>
                需要你确认这条命令
            </div>
            <div v-if="pending.ask.why" class="exec-why">{{ pending.ask.why }}</div>
            <div v-if="pending.ask.reason" class="exec-reason">{{ pending.ask.reason }}</div>
            <el-input v-model="pending.edited" type="textarea" :autosize="{ minRows: 2, maxRows: 6 }"
                class="ask-input" @keydown.enter.exact.prevent="approve" />
            <div class="ask-actions">
                <el-button size="small" type="primary" @click="approve">批准执行</el-button>
                <el-button size="small" @click="reject">拒绝</el-button>
            </div>
        </div>

        <!-- 输入区 -->
        <div class="composer">
            <el-input v-model="input" type="textarea" :autosize="{ minRows: 1, maxRows: 6 }" resize="none"
                :placeholder="sessionId ? '描述你的目标，Enter 发送，Shift+Enter 换行' : '请先连接 SSH 会话'"
                :disabled="!sessionId" @keydown.enter.exact.prevent="send" />
            <div class="composer-bar">
                <el-select v-model="model" size="small" class="ai-model" filterable allow-create default-first-option
                    placeholder="选择模型" title="模型" @change="onModelChange">
                    <el-option v-for="m in modelOptions" :key="m" :label="m" :value="m" />
                </el-select>
                <el-select v-model="authMode" size="small" class="ai-auth" :disabled="running" title="授权模式">
                    <el-option label="仅可查看" value="ask" />
                    <el-option label="敏感操作询问" value="sensitive" />
                    <el-option label="完全授权" value="full" />
                </el-select>
                <span class="bar-spacer" />
                <el-button size="small" text :disabled="!entries.length" @click="clear">清空</el-button>
                <button v-if="running" class="send stop" title="停止" @click="stop">
                    <el-icon>
                        <VideoPause />
                    </el-icon>
                </button>
                <button v-else class="send" :disabled="!canSend" title="发送" @click="send">
                    <el-icon>
                        <Promotion />
                    </el-icon>
                </button>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watchEffect } from 'vue'
import { Events, Clipboard } from '@wailsio/runtime'
import { ElMessage } from 'element-plus'
import {
    MagicStick,
    Promotion,
    VideoPause,
    ArrowDown,
    ArrowRight,
    CopyDocument,
    DocumentCopy,
    WarnTriangleFilled,
} from '@element-plus/icons-vue'
import { MdPreview } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import { AgentService, AIService, EVENTS, type AgentAsk, type AgentDone, type AgentOutput, type AgentReply, type AgentStep } from '../utils/wails'
import { showConfirmDialog } from '../utils/dialog'
import { useSettingsStore } from '../stores/settings'

const props = defineProps<{ sessionId: string }>()

const settings = useSettingsStore()
const mdTheme = computed(() => (settings.theme === 'dark' ? 'dark' : 'light'))

type ExecStatus = 'pending' | 'running' | 'done' | 'rejected' | 'blocked'

type ExecEntry = {
    kind: 'exec'
    step: number
    command: string
    reason?: string
    why?: string
    status: ExecStatus
    output: string
    exitCode: number | null
    collapsed: boolean
    startedAt?: number
    endedAt?: number
}
type ReplyEntry = { kind: 'reply'; text: string; streaming: boolean }
type Entry =
    | { kind: 'user'; text: string }
    | ReplyEntry
    | { kind: 'notice'; text: string; error?: boolean }
    | ExecEntry

// 空状态建议：覆盖最常见的几类诉求，点一下就直接发出去
const suggestions = [
    '磁盘占用情况',
    'nginx 在运行吗，占多少内存',
    '最近有哪些错误日志',
    '内存占用最高的 5 个进程',
]

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
// 正在思考的提示文案（thinking 事件带过来的说明）
const thinkingBySession = reactive<Record<string, string | null>>({})
const startedBySession = reactive<Record<string, number>>({})

const scrollRef = ref<HTMLElement | null>(null)
const stick = ref(true)
const unsubs: Array<() => void> = []

const entries = computed<Entry[]>(() => histories[props.sessionId] ?? [])
const running = computed(() => !!runningBySession[props.sessionId])
const pending = computed(() => pendingBySession[props.sessionId] ?? null)
const canSend = computed(() => input.value.trim().length > 0 && !running.value && !!props.sessionId)

const thinkingLabel = ref('')
let thinkingTimer: ReturnType<typeof setInterval> | null = null

function ensure(sid: string): Entry[] {
    if (!histories[sid]) histories[sid] = []
    return histories[sid]
}

function push(sid: string, e: Entry) {
    ensure(sid).push(e)
    scrollToLatest(true)
}

// ---------- 滚动 ----------

let scrollRafId: number | null = null

function isAtBottom(el: HTMLElement): boolean {
    return el.scrollHeight - el.scrollTop - el.clientHeight <= 32
}

function onScroll() {
    const el = scrollRef.value
    if (el) stick.value = isAtBottom(el)
}

function scrollToLatest(force = false) {
    if (force) stick.value = true
    if (!stick.value) return
    if (scrollRafId !== null) return
    scrollRafId = requestAnimationFrame(() => {
        scrollRafId = null
        const el = scrollRef.value
        if (el) el.scrollTop = el.scrollHeight
    })
}

// ---------- 操作 ----------

function onModelChange() {
    if (model.value) settings.set('ai.model', model.value).catch(() => undefined)
}

async function copyText(text: string) {
    if (!text) return
    try {
        await Clipboard.SetText(text)
        ElMessage.success('已复制')
    } catch (e: any) {
        ElMessage.error(`复制失败：${e?.message || e}`)
    }
}

function sendSuggestion(text: string) {
    input.value = text
    void send()
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
    startedBySession[props.sessionId] = Date.now()
    thinkingBySession[props.sessionId] = '正在思考'
    try {
        await AgentService.Send(props.sessionId, text, authMode.value)
    } catch (e: any) {
        runningBySession[props.sessionId] = false
        thinkingBySession[props.sessionId] = null
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
    thinkingBySession[props.sessionId] = null
    await AgentService.Clear(props.sessionId).catch(() => undefined)
}

// ---------- 事件 ----------

function lastExec(sid: string): ExecEntry | null {
    const list = histories[sid] ?? []
    for (let i = list.length - 1; i >= 0; i--) {
        if (list[i].kind === 'exec') return list[i] as ExecEntry
    }
    return null
}

function lastReply(sid: string): ReplyEntry | null {
    const list = histories[sid] ?? []
    const last = list[list.length - 1]
    return last && last.kind === 'reply' ? (last as ReplyEntry) : null
}

function statusText(e: ExecEntry): string {
    switch (e.status) {
        case 'pending':
            return '待确认'
        case 'running':
            return '执行中'
        case 'rejected':
            return '已拒绝'
        case 'blocked':
            return '已被规则拦截'
        case 'done': {
            const base = e.exitCode === null ? '完成' : `exit=${e.exitCode}`
            if (e.startedAt && e.endedAt) {
                const ms = e.endedAt - e.startedAt
                return `${base} · ${ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`}`
            }
            return base
        }
    }
}

function statusClass(e: ExecEntry): string {
    if (e.status === 'rejected' || e.status === 'blocked') return 'bad'
    if (e.status === 'done' && e.exitCode !== 0) return 'bad'
    if (e.status === 'pending') return 'warn'
    if (e.status === 'running') return 'run'
    return ''
}

// agent:reply —— 流式增量；done 表示这条消息结束
function onReply(evt: any) {
    const d = evt?.data as AgentReply | undefined
    if (!d?.sessionId) return
    if (d.done) {
        const last = lastReply(d.sessionId)
        if (last) last.streaming = false
        if (d.error) {
            if (last) {
                last.text = last.text ? `${last.text}\n\n> ⚠️ ${d.error}` : `⚠️ ${d.error}`
            } else {
                push(d.sessionId, { kind: 'notice', text: `⚠️ ${d.error}`, error: true })
            }
        }
        scrollToLatest()
        return
    }
    if (!d.content) return
    const last = lastReply(d.sessionId)
    if (last && last.streaming) {
        last.text += d.content
    } else {
        push(d.sessionId, { kind: 'reply', text: d.content, streaming: true })
    }
    scrollToLatest()
}

// agent:step —— thinking / propose / running / rejected / blocked
function onStep(evt: any) {
    const d = evt?.data as AgentStep | undefined
    if (!d?.sessionId) return
    const sid = d.sessionId
    switch (d.status) {
        case 'thinking':
            thinkingBySession[sid] = d.reason || '正在思考'
            break
        case 'propose':
            thinkingBySession[sid] = null
            push(sid, {
                kind: 'exec',
                step: d.step,
                command: d.command || '',
                reason: d.reason,
                why: d.why,
                status: 'pending',
                output: '',
                exitCode: null,
                collapsed: true,
            })
            break
        case 'running': {
            thinkingBySession[sid] = null
            const e = lastExec(sid)
            if (e) {
                e.status = 'running'
                e.command = d.command || e.command
                e.startedAt = Date.now()
            }
            break
        }
        case 'rejected': {
            const e = lastExec(sid)
            if (e) {
                e.status = 'rejected'
                e.collapsed = false
            }
            break
        }
        case 'blocked':
            // 被「不允许串联」之类的规则挡下的命令：展示出来，然后 AI 会自动改
            thinkingBySession[sid] = null
            push(sid, {
                kind: 'exec',
                step: d.step,
                command: d.command || '',
                reason: d.reason,
                why: d.why,
                status: 'blocked',
                output: '',
                exitCode: null,
                collapsed: false,
            })
            break
    }
}

function onAsk(evt: any) {
    const d = evt?.data as AgentAsk | undefined
    if (!d?.sessionId) return
    thinkingBySession[d.sessionId] = null
    pendingBySession[d.sessionId] = { ask: d, edited: d.command }
    scrollToLatest(true)
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
        e.endedAt = Date.now()
        // 出错时自动展开，省去一次点击
        e.collapsed = d.exitCode === 0
    }
    scrollToLatest()
}

function onDone(evt: any) {
    const d = evt?.data as AgentDone | undefined
    if (!d?.sessionId) return
    const sid = d.sessionId
    runningBySession[sid] = false
    pendingBySession[sid] = null
    thinkingBySession[sid] = null
    const last = lastReply(sid)
    if (last) last.streaming = false
    if (d.error) {
        push(sid, { kind: 'notice', text: `⚠️ ${d.error}`, error: true })
    } else if (d.summary && d.summary !== '已取消' && d.summary !== '已由用户取消') {
        push(sid, { kind: 'notice', text: d.summary })
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
    unsubs.push(Events.On(EVENTS.sessionClosed, (evt: any) => {
        const sid = evt?.data?.sessionId
        if (sid) {
            delete histories[sid]
            delete runningBySession[sid]
            delete pendingBySession[sid]
            delete thinkingBySession[sid]
            delete startedBySession[sid]
        }
    }))
})

// 只在对话进行中开启计时器更新「思考中（N s）」文案，闲着不占资源
watchEffect((onCleanup) => {
    if (!running.value) {
        thinkingLabel.value = ''
        return
    }
    const sid = props.sessionId
    const update = () => {
        const base = thinkingBySession[sid]
        if (!base) { thinkingLabel.value = ''; return }
        const started = startedBySession[sid]
        if (!started) { thinkingLabel.value = base; return }
        const secs = Math.max(1, Math.round((Date.now() - started) / 1000))
        thinkingLabel.value = `${base}（${secs}s）`
    }
    update()
    thinkingTimer = setInterval(update, 1000)
    onCleanup(() => {
        if (thinkingTimer !== null) {
            clearInterval(thinkingTimer)
            thinkingTimer = null
        }
    })
})

onBeforeUnmount(() => {
    unsubs.forEach((u) => u())
    if (scrollRafId !== null) cancelAnimationFrame(scrollRafId)
    if (thinkingTimer !== null) {
        clearInterval(thinkingTimer)
        thinkingTimer = null
    }
})
</script>

<style scoped>
.ai-chat {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    position: relative;
}

/* ---------- 顶部 ---------- */

.ai-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.ai-title {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 12.5px;
    font-weight: 600;
    color: var(--text-primary);
    flex-shrink: 0;
}

.ai-model {
    width: 140px;
    flex-shrink: 0;
}

/* ---------- 消息区 ---------- */

.ai-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 12px 10px 4px;
    display: flex;
    flex-direction: column;
    gap: 12px;
}

.row {
    display: flex;
    gap: 8px;
    align-items: flex-start;
    max-width: 100%;
}

.row.end {
    justify-content: flex-end;
}

.avatar {
    flex-shrink: 0;
    width: 24px;
    height: 24px;
    border-radius: 50%;
    background: var(--active-bg);
    color: var(--active-text);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 13px;
    margin-top: 2px;
}

.bubble.user {
    max-width: 84%;
    padding: 7px 11px;
    border-radius: 12px 12px 3px 12px;
    background: var(--active-bg);
    color: var(--active-text);
    font-size: 12.5px;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-word;
}

.ai-msg {
    flex: 1;
    min-width: 0;
    font-size: 12.5px;
    line-height: 1.65;
    color: var(--text-primary);
    position: relative;
}

/* Markdown 预览：去掉 md-editor 自带的边框/背景/内边距，融进对话流 */
.ai-msg :deep(.md-body),
.ai-msg :deep(.md-editor-preview-wrapper) {
    padding: 0;
    margin: 0;
    background: transparent;
    color: var(--text-primary);
    font-size: 12.5px;
    line-height: 1.65;
}

.ai-msg :deep(.md-editor-preview) {
    font-size: 12.5px;
    line-height: 1.65;
    background: transparent;
    color: var(--text-primary);
    word-break: break-word;
}

.ai-msg :deep(.md-editor-preview h1),
.ai-msg :deep(.md-editor-preview h2),
.ai-msg :deep(.md-editor-preview h3),
.ai-msg :deep(.md-editor-preview h4) {
    font-size: 13.5px;
    margin: 10px 0 6px;
    border-bottom: none;
    padding-bottom: 0;
}

.ai-msg :deep(.md-editor-preview p) {
    margin: 6px 0;
}

.ai-msg :deep(.md-editor-preview ul),
.ai-msg :deep(.md-editor-preview ol) {
    padding-left: 20px;
    margin: 6px 0;
}

.ai-msg :deep(.md-editor-preview pre) {
    background: var(--term-bg);
    border-radius: 6px;
    padding: 8px 10px;
    margin: 8px 0;
    overflow-x: auto;
}

.ai-msg :deep(.md-editor-preview code) {
    font-family: var(--term-font);
    font-size: 11.5px;
}

.ai-msg :deep(.md-editor-preview table) {
    border-collapse: collapse;
    font-size: 12px;
}

.ai-msg :deep(.md-editor-preview th),
.ai-msg :deep(.md-editor-preview td) {
    border: 1px solid var(--border-color);
    padding: 3px 7px;
}

.ai-msg :deep(.md-editor-preview blockquote) {
    margin: 6px 0;
    padding-left: 10px;
    border-left: 3px solid var(--border-strong);
    color: var(--text-secondary);
}

.caret {
    display: inline-block;
    width: 7px;
    height: 13px;
    margin-left: 2px;
    vertical-align: text-bottom;
    background: var(--accent);
    animation: blink 1s steps(2, start) infinite;
}

@keyframes blink {
    50% {
        opacity: 0;
    }
}

.msg-actions {
    display: flex;
    gap: 2px;
    margin-top: 2px;
    opacity: 0;
    transition: opacity 0.15s;
}

.ai-msg:hover .msg-actions {
    opacity: 1;
}

.mini {
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    padding: 1px 3px;
    border-radius: 4px;
    font-size: 11px;
    display: inline-flex;
    align-items: center;
}

.mini:hover {
    color: var(--text-primary);
    background: var(--hover-bg);
}

/* ---------- 空状态 ---------- */

.ai-empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    text-align: center;
    padding: 0 8px;
}

.empty-title {
    font-size: 12.5px;
    color: var(--text-secondary);
    line-height: 1.6;
}

.chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    justify-content: center;
}

.chip {
    font-size: 12px;
    padding: 4px 10px;
    border-radius: 999px;
    border: 1px solid var(--border-color);
    background: transparent;
    color: var(--text-secondary);
    cursor: pointer;
    transition: all 0.15s;
}

.chip:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
}

.chip:disabled {
    opacity: 0.45;
    cursor: not-allowed;
}

/* ---------- 提示 / 思考中 ---------- */

.notice {
    align-self: center;
    font-size: 11.5px;
    color: var(--text-muted);
    text-align: center;
    max-width: 92%;
}

.notice.err {
    color: #f56c6c;
}

.thinking {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: 12.5px;
    color: var(--text-secondary);
    padding-top: 3px;
}

.dots {
    display: inline-flex;
    gap: 3px;
}

.dots i {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--accent);
    animation: bounce 1.2s infinite ease-in-out;
}

.dots i:nth-child(2) {
    animation-delay: 0.15s;
}

.dots i:nth-child(3) {
    animation-delay: 0.3s;
}

@keyframes bounce {

    0%,
    60%,
    100% {
        opacity: 0.3;
        transform: translateY(0);
    }

    30% {
        opacity: 1;
        transform: translateY(-3px);
    }
}

/* ---------- 命令卡片 ---------- */

.exec {
    border: 1px solid var(--border-color);
    background: var(--term-bg);
    border-radius: 8px;
    padding: 6px 9px;
    max-width: 100%;
    min-width: 0;
}

.exec.pending {
    border-color: #e6a23c;
}

.exec.rejected,
.exec.blocked {
    border-color: rgba(245, 108, 108, 0.55);
}

.exec-head {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    cursor: pointer;
}

.exec-caret {
    flex-shrink: 0;
    color: var(--text-muted);
    margin-top: 2px;
    font-size: 12px;
}

.exec-cmd {
    flex: 1;
    min-width: 0;
    font-size: 12px;
    color: var(--text-primary);
    word-break: break-all;
    white-space: pre-wrap;
}

.exec-status {
    flex-shrink: 0;
    font-size: 11px;
    color: var(--text-muted);
}

.exec-status.run {
    color: var(--accent);
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
    margin-top: 4px;
    padding-left: 18px;
}

.exec-why {
    font-size: 11.5px;
    color: #e6a23c;
    margin-top: 3px;
    padding-left: 18px;
}

.exec-body {
    margin-top: 5px;
    padding-left: 18px;
}

.exec-tools {
    display: flex;
    justify-content: flex-end;
    gap: 2px;
}

.exec-output {
    margin: 2px 0 0;
    font-size: 11.5px;
    line-height: 1.5;
    color: var(--text-primary);
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 220px;
    overflow-y: auto;
    background: var(--hover-bg);
    border-radius: 5px;
    padding: 6px 8px;
}

.exec-running {
    font-size: 11.5px;
    color: var(--text-muted);
    padding: 2px 0;
}

/* ---------- 回到底部 ---------- */

.to-bottom {
    position: absolute;
    right: 14px;
    bottom: 116px;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    border: 1px solid var(--border-color);
    background: var(--panel-bg);
    color: var(--text-secondary);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.25);
    z-index: 5;
}

.to-bottom:hover {
    color: var(--accent);
    border-color: var(--accent);
}

/* ---------- 待确认 ---------- */

.ask {
    flex-shrink: 0;
    margin: 0 10px 8px;
    border: 1px solid #e6a23c;
    background: rgba(230, 162, 60, 0.08);
    border-radius: 8px;
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 6px;
}

.ask-title {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 12.5px;
    font-weight: 600;
    color: #e6a23c;
}

.ask .exec-reason,
.ask .exec-why {
    padding-left: 0;
    margin-top: 0;
}

.ask-input :deep(.el-textarea__inner) {
    font-size: 12px;
    font-family: var(--term-font);
    line-height: 1.5;
}

.ask-actions {
    display: flex;
    gap: 6px;
}

/* ---------- 输入区 ---------- */

.composer {
    flex-shrink: 0;
    margin: 0 10px 10px;
    border: 1px solid var(--border-color);
    border-radius: 10px;
    background: var(--panel-bg);
    padding: 6px 8px 4px;
    transition: border-color 0.15s;
}

.composer:focus-within {
    border-color: var(--accent);
}

.composer :deep(.el-textarea__inner) {
    font-size: 12.5px;
    line-height: 1.55;
    padding: 2px 2px 4px;
    box-shadow: none;
    background: transparent;
}

.composer-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-top: 2px;
}

.bar-spacer {
    flex: 1;
}

.ai-auth {
    width: 118px;
}

.send {
    width: 26px;
    height: 26px;
    border-radius: 50%;
    border: none;
    background: var(--accent);
    color: #fff;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 13px;
    flex-shrink: 0;
}

.send:disabled {
    opacity: 0.4;
    cursor: not-allowed;
}

.send.stop {
    background: #f56c6c;
}

.send:not(:disabled):hover {
    filter: brightness(1.12);
}
</style>