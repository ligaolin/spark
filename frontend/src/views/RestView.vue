<template>
    <div class="rest-view">
        <el-splitter :layout="'horizontal'" class="root-splitter">
            <el-splitter-panel :size="sidebarSize" :min="180" :max="520" @resize="onSidebarResize">
                <div class="rest-sidebar">
                    <div class="sidebar-search">
                        <el-input v-model="treeFilter" size="small" placeholder="搜索请求..." clearable>
                            <template #prefix>
                                <el-icon>
                                    <Search />
                                </el-icon>
                            </template>
                        </el-input>
                    </div>

                    <div class="sidebar-tree" @contextmenu.prevent="onTreeContext">
                        <el-tree ref="treeRef" :data="treeData" node-key="nodeKey"
                            :props="{ label: 'name', children: 'children', isLeaf: 'leaf' }" highlight-current
                            :expand-on-click-node="true" lazy :load="loadNode" :filter-node-method="filterTreeNode"
                            draggable :allow-drag="allowDrag" :allow-drop="allowDrop"
                            @node-click="onNodeClick" @node-contextmenu="onNodeContext"
                            @node-drag-end="onNodeDragEnd" empty-text="暂无请求，右键新建">
                            <template #default="{ data }">
                                <span class="tree-node">
                                    <el-icon :color="data.type === 'folder' ? '#e6c06c' : ''">
                                        <Folder v-if="data.type === 'folder'" />
                                        <Promotion v-else />
                                    </el-icon>
                                    <span v-if="data.type === 'request'" class="tree-method"
                                        :class="'method-' + (data.method || 'get').toLowerCase()">{{ data.method
                                        }}</span>
                                    <el-input v-if="renamingNodeKey === data.nodeKey" :ref="setRenameInputRef"
                                        v-model="renameDraft" size="small" class="rest-rename-input"
                                        @click.stop @mousedown.stop @dblclick.stop
                                        @keyup.enter="onRenameEnter" @keyup.esc="cancelRename"
                                        @blur="commitRename" />
                                    <span v-else class="tree-node-name" :title="data.name">{{ data.name }}</span>
                                    <span v-if="data.type === 'folder' && data.activeBaseUrl"
                                        class="tree-base-url-tag">{{ data.activeBaseUrl }}</span>
                                </span>
                            </template>
                        </el-tree>
                    </div>
                </div>
            </el-splitter-panel>

            <!-- 主区域 -->
            <el-splitter-panel :min="300" collapsible>
                <div class="rest-main">
                    <!-- 顶部工具栏 -->
                    <div class="rest-toolbar">
                        <el-select v-model="current.method" class="method-select" :class="methodClass">
                            <el-option v-for="m in methods" :key="m" :label="m" :value="m" />
                        </el-select>
                        <el-input v-model="current.url" class="url-input"
                            :placeholder="current.method === 'WS' ? 'ws://localhost:8080/ws' : '输入请求 URL，例如 /api/users'"
                            clearable :disabled="current.method === 'WS' && wsConnected" @keydown.enter="send" />
                        <el-popover v-if="current.method !== 'WS'" placement="bottom" :width="260" trigger="hover"
                            :show-after="300">
                            <template #reference>
                                <el-button :type="current.insecure ? 'warning' : ''" text size="small"
                                    @click="toggleInsecure">
                                    <el-icon>
                                        <Lock />
                                    </el-icon>
                                </el-button>
                            </template>
                            <div style="font-size:12px;line-height:1.6">
                                <template v-if="current.insecure">
                                    <span style="color:#e6a23c">⚠ 已跳过 TLS 证书验证</span><br />
                                    不验证服务器证书，适用于自签名证书或测试环境。
                                </template>
                                <template v-else>
                                    点击锁图标可跳过 TLS 证书验证，<br />适用于自签名证书或测试环境。
                                </template>
                            </div>
                        </el-popover>
                        <el-button v-if="current.method !== 'WS'" text size="small" title="生成代码" @click="genCode">
                            <el-icon>
                                <CopyDocument />
                            </el-icon>
                        </el-button>
                        <el-button v-if="current.method !== 'WS'" type="primary" :loading="sending" @click="send">
                            <el-icon v-if="!sending">
                                <Promotion />
                            </el-icon>
                            <span>发送</span>
                        </el-button>
                        <el-button v-else-if="!wsConnected" type="primary" :icon="Connection" @click="wsDoConnect">
                            连接
                        </el-button>
                        <el-button v-else type="danger" :icon="Setting" @click="wsDoClose">
                            断开
                        </el-button>
                        <el-button @click="saveCurrentRequest" :loading="saving">
                            <el-icon><Select /></el-icon>
                            <span>保存</span>
                        </el-button>
                        <el-dropdown trigger="click" class="toolbar-more">
                            <el-button text>
                                <el-icon>
                                    <MoreFilled />
                                </el-icon>
                            </el-button>
                            <template #dropdown>
                                <el-dropdown-menu>
                                    <el-dropdown-item @click="openHistory">
                                        <el-icon>
                                            <Clock />
                                        </el-icon>请求历史
                                    </el-dropdown-item>
                                    <el-dropdown-item @click="openCurlImport">
                                        <el-icon>
                                            <Download />
                                        </el-icon>导入 curl
                                    </el-dropdown-item>
                                    <el-dropdown-item divided @click="showStressDialog = true">
                                        <el-icon>
                                            <TrendCharts />
                                        </el-icon>压力测试
                                    </el-dropdown-item>
                                </el-dropdown-menu>
                            </template>
                        </el-dropdown>
                    </div>

                    <!-- 环境提示 -->
                    <div class="env-hint" v-if="effectiveDisplayBase && current.url && !isAbsoluteURL(current.url)">
                        <el-icon>
                            <Connection />
                        </el-icon>
                        <span>{{ effectiveDisplayBase }}{{ current.url }}</span>
                    </div>

                    <el-splitter layout="vertical" class="body-splitter">
                        <el-splitter-panel :size="requestSize" :min="120" :max="'85%'" @resize="onRequestResize">
                            <div class="rest-request">
                                <el-tabs v-model="reqTab" class="rest-tabs">
                                    <el-tab-pane label="Headers" name="headers">
                                        <div class="kv-editor">
                                            <div class="kv-row" v-for="(row, i) in current.headers" :key="i">
                                                <el-checkbox v-model="row.enabled" class="kv-check" />
                                                <el-input v-model="row.key" class="kv-key" placeholder="Header 名" />
                                                <el-input v-model="row.value" class="kv-val" placeholder="Header 值" />
                                                <el-button size="small" :icon="Delete" circle class="kv-del"
                                                    @click="current.headers.splice(i, 1)" />
                                            </div>
                                            <el-button size="small" :icon="Plus"
                                                @click="current.headers.push({ key: '', value: '', enabled: true })">
                                                添加 Header
                                            </el-button>
                                        </div>
                                    </el-tab-pane>
                                    <el-tab-pane label="Query" name="query">
                                        <div class="kv-editor">
                                            <div class="kv-row" v-for="(row, i) in current.params" :key="i">
                                                <el-checkbox v-model="row.enabled" class="kv-check" />
                                                <el-input v-model="row.key" class="kv-key" placeholder="参数名" />
                                                <el-input v-model="row.value" class="kv-val" placeholder="参数值" />
                                                <el-button size="small" :icon="Delete" circle class="kv-del"
                                                    @click="current.params.splice(i, 1)" />
                                            </div>
                                            <el-button size="small" :icon="Plus"
                                                @click="current.params.push({ key: '', value: '', enabled: true })">
                                                添加参数
                                            </el-button>
                                        </div>
                                    </el-tab-pane>
                                    <el-tab-pane label="Body" name="body">
                                        <div class="body-type-bar">
                                            <el-radio-group v-model="bodyType" size="small">
                                                <el-radio-button value="json">JSON</el-radio-button>
                                                <el-radio-button value="raw">Raw</el-radio-button>
                                                <el-radio-button value="form">Form-Data</el-radio-button>
                                                <el-radio-button value="urlencoded">Urlencoded</el-radio-button>
                                                <el-radio-button value="binary">Binary</el-radio-button>
                                            </el-radio-group>
                                            <el-button v-if="bodyType === 'json'" size="small" text title="格式化请求 Body"
                                                @click="bodyEditorRef?.format()" style="vertical-align: bottom;margin-left: 8px">
                                                <el-icon>
                                                    <MagicStick />
                                                </el-icon>
                                            </el-button>
                                        </div>
                                        <div class="code-wrap" v-if="bodyType === 'json'">
                                            <CodeEditor ref="bodyEditorRef" filename="request.json" wrap />
                                        </div>
                                        <div class="code-wrap" v-else-if="bodyType === 'raw'">
                                            <CodeEditor ref="rawBodyEditorRef" filename="request.txt" wrap />
                                        </div>
                                        <div class="kv-editor" v-else-if="bodyType === 'urlencoded'">
                                            <div class="kv-row" v-for="(row, i) in urlencodedFields" :key="'u' + i">
                                                <el-checkbox v-model="row.enabled" class="kv-check" />
                                                <el-input v-model="row.key" class="kv-key" placeholder="参数名" />
                                                <el-input v-model="row.value" class="kv-val" placeholder="参数值" />
                                                <el-button size="small" :icon="Delete" circle
                                                    @click="urlencodedFields.splice(i, 1)" />
                                            </div>
                                            <el-button size="small" :icon="Plus"
                                                @click="urlencodedFields.push({ key: '', value: '', enabled: true })">添加参数</el-button>
                                        </div>
                                        <div class="form-data-editor" v-else-if="bodyType === 'form'">
                                            <div class="kv-editor" style="height:auto;max-height:50%">
                                                <div class="kv-row" v-for="(row, i) in formFields" :key="'f' + i">
                                                    <el-checkbox v-model="row.enabled" class="kv-check" />
                                                    <el-input v-model="row.key" class="kv-key" placeholder="字段名" />
                                                    <el-input v-model="row.value" class="kv-val" placeholder="值" />
                                                    <el-button size="small" :icon="Delete" circle
                                                        @click="formFields.splice(i, 1)" />
                                                </div>
                                                <el-button size="small" :icon="Plus"
                                                    @click="formFields.push({ key: '', value: '', enabled: true })">添加字段</el-button>
                                            </div>
                                            <div class="form-files">
                                                <div class="form-file-row" v-for="(f, i) in formFiles"
                                                    :key="'file' + i">
                                                    <el-input v-model="f.fieldName" class="kv-key" placeholder="字段名" />
                                                    <el-button size="small" @click="selectFormFile(i)">{{ f.fileName ||
                                                        '选择文件'
                                                    }}</el-button>
                                                    <span class="form-file-name" v-if="f.fileName">{{ f.fileName
                                                    }}</span>
                                                    <el-button size="small" :icon="Delete" circle
                                                        @click="formFiles.splice(i, 1)" />
                                                </div>
                                                <el-button size="small" :icon="Plus"
                                                    @click="formFiles.push({ fieldName: 'file', fileName: '', filePath: '' })">添加文件</el-button>
                                                <el-button size="small" @click="selectFormFiles">批量选择</el-button>
                                            </div>
                                        </div>
                                        <div class="binary-editor" v-else-if="bodyType === 'binary'">
                                            <el-button size="small" @click="selectBinaryFile">选择文件</el-button>
                                            <span class="form-file-name" v-if="binaryFilePath">{{ binaryFilePath
                                            }}</span>
                                        </div>
                                    </el-tab-pane>
                                </el-tabs>
                            </div>
                        </el-splitter-panel>

                        <el-splitter-panel :min="120">
                            <div class="rest-response">
                                <div class="response-status-bar" v-if="response">
                                    <el-tag :type="statusTagType" size="small" effect="dark">
                                        {{ response.status }} {{ response.statusText }}
                                    </el-tag>
                                    <span class="response-duration">{{ response.duration }} ms</span>
                                    <span class="response-size">{{ formatSize(response.body?.length || 0) }}</span>
                                    <el-button v-if="sseTimer || sseStopped" size="small" text
                                        :type="sseStopped ? 'info' : 'danger'"
                                        :title="sseStopped ? '流式数据接收已停止' : '停止接收流式数据'"
                                        :disabled="sseStopped"
                                        @click="stopStreaming">
                                        <el-icon><CloseBold /></el-icon> {{ sseStopped ? '已停止' : '停止' }}
                                    </el-button>
                                    <el-button v-if="isHtmlResponse" size="small" text class="html-toggle"
                                        @click="htmlPreview = !htmlPreview">
                                        {{ htmlPreview ? '查看源码' : '页面预览' }}
                                    </el-button>
                                    <el-button v-if="response && resTab === 'body' && !isHtmlResponse" size="small" text
                                        title="格式化响应 Body" @click="respEditorRef?.format()">
                                        <el-icon>
                                            <MagicStick />
                                        </el-icon>
                                    </el-button>
                                </div>
                                <el-tabs v-model="resTab" class="rest-tabs">
                                    <el-tab-pane label="Body" name="body">
                                        <template v-if="current.method === 'WS'">
                                            <template v-if="wsConnected">
                                                <div class="ws-log" ref="wsLogRef">
                                                    <div v-if="!wsMessages.length" class="ws-empty">暂无消息</div>
                                                    <div v-for="(m, i) in wsMessages" :key="i" class="ws-msg"
                                                        :class="{ sent: m.sent, close: m.type === 'close' }">
                                                        <span class="ws-ts">{{ new Date(m.time).toLocaleTimeString()
                                                        }}</span>
                                                        <span class="ws-dir">{{ m.sent ? '>>' : (m.type === 'close' ?
                                                            '×' : '<<') }}</span>
                                                                <span class="ws-type"
                                                                    v-if="m.type === 'close'">连接关闭</span>
                                                                <pre class="ws-data">{{ m.data }}</pre>
                                                    </div>
                                                </div>
                                                <div class="ws-send-bar">
                                                    <el-input v-model="wsMsgInput" type="textarea"
                                                        :autosize="{ minRows: 1, maxRows: 4 }"
                                                        @keydown.enter.exact.prevent="wsDoSend"
                                                        placeholder="输入消息，Enter 发送" />
                                                    <el-button type="primary" @click="wsDoSend"
                                                        :disabled="!wsMsgInput.trim()">发送</el-button>
                                                    <el-button size="small" text @click="wsMessages = []">清空</el-button>
                                                </div>
                                            </template>
                                            <div v-else class="response-empty-hint">
                                                <el-icon :size="36" color="var(--border-strong)">
                                                    <Connection />
                                                </el-icon>
                                                <p>点击发送按钮建立 WebSocket 连接</p>
                                            </div>
                                        </template>
                                        <template v-else-if="response">
                                            <div class="html-preview-wrap" v-if="isHtmlResponse && htmlPreview">
                                                <iframe class="html-preview-iframe" :srcdoc="response.body"
                                                    sandbox="allow-same-origin allow-scripts" />
                                            </div>
                                            <div class="code-wrap" v-else>
                                                <CodeEditor ref="respEditorRef" :key="'resp-' + responseSeq"
                                                    :filename="respFilename" wrap />
                                            </div>
                                        </template>
                                        <div v-else class="response-empty-hint">
                                            <el-icon :size="36" color="var(--border-strong)">
                                                <Promotion />
                                            </el-icon>
                                            <p>{{ sending ? '请求中...' : '点击发送按钮发起请求' }}</p>
                                        </div>
                                    </el-tab-pane>
                                    <el-tab-pane label="Headers" name="resHeaders">
                                        <template v-if="current.method === 'WS'">
                                            <div class="kv-editor readonly">
                                                <div class="kv-row" v-for="(val, key) in (wsHandshakeHeaders || {})"
                                                    :key="key">
                                                    <span class="kv-key-ro">{{ key }}</span>
                                                    <span class="kv-val-ro">{{ val }}</span>
                                                </div>
                                                <p v-if="!Object.keys(wsHandshakeHeaders || {}).length"
                                                    class="kv-empty">无握手响应头</p>
                                            </div>
                                        </template>
                                        <template v-else-if="response">
                                            <div class="kv-editor readonly">
                                                <div class="kv-row" v-for="(val, key) in (response.headers || {})"
                                                    :key="key">
                                                    <span class="kv-key-ro">{{ key }}</span>
                                                    <span class="kv-val-ro">{{ val }}</span>
                                                </div>
                                                <p v-if="!Object.keys(response.headers || {}).length" class="kv-empty">
                                                    无响应头</p>
                                            </div>
                                        </template>
                                        <div v-else class="response-empty-hint">
                                            <el-icon :size="36" color="var(--border-strong)">
                                                <Promotion />
                                            </el-icon>
                                            <p>{{ current.method === 'WS' ? '连接后显示握手头' : '发送请求后显示响应头' }}</p>
                                        </div>
                                    </el-tab-pane>
                                </el-tabs>
                            </div>
                        </el-splitter-panel>
                    </el-splitter>
                </div>
            </el-splitter-panel>
        </el-splitter>

        <!-- 设置环境弹窗（目录右键：基础链接 + 公共请求头） -->
        <RestEnvDialog v-model="showEnvDialog" :node="envTargetNode" @saved="onEnvSaved" />

        <!-- curl 导入弹窗 -->
        <RestCurlImportDialog v-model="showCurlDialog" @import="applyCurlImport" />

        <!-- curl 生成弹窗 -->
        <RestGenCodeDialog v-model="showGenCurlDialog" v-model:language="codeLanguage" :code="generatedCurl" />

        <!-- 压力测试弹窗 -->
        <RestStressDialog v-model="showStressDialog" :running="stressRunning" :progress="stressProgress"
            :result="stressResult" :latency-rows="latencyRows" :status-rows="statusRows" :url="stressUrl"
            @start="runStress" @stop="stopStress" />

        <!-- 请求历史抽屉 -->
        <el-drawer v-model="historyOpen" title="请求历史" size="520px">
            <div class="history-toolbar">
                <span class="history-count">共 {{ history.length }} 条 · 最多保留 {{ HISTORY_MAX }} 条</span>
                <el-button size="small" text type="danger" :disabled="!history.length" @click="clearHistory">清空</el-button>
            </div>
            <div v-if="!history.length" class="history-empty">暂无记录，发送请求后会自动记录</div>
            <div v-for="(h, i) in history" :key="h.id" class="history-item" @click="applyHistory(h)">
                <div class="history-line1">
                    <span class="tree-method" :class="'method-' + h.method.toLowerCase()">{{ h.method }}</span>
                    <span class="history-url" :title="h.resolvedUrl">{{ h.resolvedUrl }}</span>
                    <el-icon class="history-del" title="删除该记录" @click.stop="removeHistory(i)">
                        <Delete />
                    </el-icon>
                </div>
                <div class="history-line2">
                    <span class="history-time">{{ fmtHistoryTime(h.time) }}</span>
                    <span v-if="h.error" class="history-status err" :title="h.error">失败</span>
                    <span v-else class="history-status"
                        :class="h.status >= 400 ? 'err' : h.status >= 300 ? 'warn' : 'ok'">
                        {{ h.status }} · {{ h.duration }}ms
                    </span>
                </div>
            </div>
        </el-drawer>

        <!-- 右键菜单 -->
        <ContextMenu v-model="ctxVisible" :x="ctxX" :y="ctxY" :items="ctxItems" @pick="onCtxPick" />
    </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onActivated, onDeactivated, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Events } from '@wailsio/runtime'
import { Delete, Select, Plus, Promotion, Setting, Connection, Folder, Download, CopyDocument, MoreFilled, TrendCharts, Search, Lock, MagicStick, Refresh, CloseBold, Edit, Clock } from '@element-plus/icons-vue'
import CodeEditor from '../components/CodeEditor.vue'
import RestEnvDialog from '../components/rest/RestEnvDialog.vue'
import RestCurlImportDialog from '../components/rest/RestCurlImportDialog.vue'
import RestGenCodeDialog from '../components/rest/RestGenCodeDialog.vue'
import RestStressDialog from '../components/rest/RestStressDialog.vue'
import { useSettingsStore } from '../stores/settings'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import { EVENTS } from '../utils/wails'
import { stripJsonComments } from '../utils/monaco'
import { safeDecode, type CurlParseResult, type KV } from '../utils/restCurl'
import { buildCode, type CodeLanguage } from '../utils/restCodegen'
import * as RestService from '../../bindings/spark/app/service/rest/restservice.js'
import type { RestRequest, RestResponse, StressTestRequest, StressTestResult, FormKV } from '../../bindings/spark/app/service/types/models.js'

interface StressProgress {
    current: number
    total: number
}

interface TreeNode {
    nodeKey: string
    id: number
    parentId: number
    name: string
    type: string
    method?: string
    url?: string
    baseUrl?: string
    activeBaseUrl?: string
    commonHeaders?: string
    leaf: boolean
    sort: number
    children?: TreeNode[]
}

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS', 'WS']

const settings = useSettingsStore()

const current = ref({
    method: 'GET',
    url: '',
    headers: [] as KV[],
    params: [] as KV[],
    body: '',
    insecure: false,
})

const reqTab = ref('headers')
const resTab = ref('body')
const sending = ref(false)
const saving = ref(false)
const response = ref<RestResponse | null>(null)
const responseSeq = ref(0)
const bodyEditorRef = ref<InstanceType<typeof CodeEditor> | null>(null)
const rawBodyEditorRef = ref<InstanceType<typeof CodeEditor> | null>(null)
const respEditorRef = ref<InstanceType<typeof CodeEditor> | null>(null)

// 树
const treeRef = ref()
const treeData = ref<TreeNode[]>([])
const treeFilter = ref('')
const editingNode = ref<TreeNode | null>(null)
const editingRequestId = ref<number>(0)
const folderEffectiveBaseUrl = ref('')
const folderCommonHeaders = ref<KV[]>([])

// 行内重命名（VSCode 式）：编辑中的节点 key 与输入内容
const renamingNodeKey = ref('')
const renameDraft = ref('')
const renameTarget = ref<TreeNode | null>(null)
const renameInputRef = ref<{ focus: () => void; select?: () => void } | null>(null)

function setRenameInputRef(el: any) {
    renameInputRef.value = el
}

// 右键菜单
const ctxVisible = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)
const ctxNode = ref<TreeNode | null>(null)
const ctxItems = ref<(CtxItem | 'divider')[]>([])

// 复制/粘贴
const copiedNode = ref<TreeNode | null>(null)

// 压力测试
const showStressDialog = ref(false)
const stressRunning = ref(false)
const stressProgress = ref<StressProgress>({ current: 0, total: 0 })
const stressResult = ref<StressTestResult | null>(null)
let stressTimer: ReturnType<typeof setInterval> | null = null

// 设置环境弹窗（目录右键：基础链接 + 公共请求头）
const showEnvDialog = ref(false)
const envTargetNode = ref<TreeNode | null>(null)

// HTML 预览
const htmlPreview = ref(true)
const isHtmlResponse = computed(() => {
    if (!response.value) return false
    const ct = (response.value.headers || {})['Content-Type'] || (response.value.headers || {})['content-type'] || ''
    return ct.includes('html')
})

// Body 类型（JSON / Raw / Form-Data / Form-Urlencoded / Binary）
const bodyType = ref<'json' | 'raw' | 'form' | 'urlencoded' | 'binary'>('json')
const rawBody = ref('')

// 切换 body 类型时，保存旧编辑器内容、恢复新编辑器内容
watch(bodyType, (newType, oldType) => {
    if (oldType === 'json') {
        current.value.body = bodyEditorRef.value?.getContent() ?? current.value.body
    } else if (oldType === 'raw') {
        rawBody.value = rawBodyEditorRef.value?.getContent() ?? rawBody.value
    }
    nextTick(() => {
        if (newType === 'json') {
            bodyEditorRef.value?.setContent(current.value.body)
        } else if (newType === 'raw') {
            rawBodyEditorRef.value?.setContent(rawBody.value)
        }
    })
})
const formFields = ref<FormKV[]>([])
const urlencodedFields = ref<FormKV[]>([])
const formFiles = ref<{ fieldName: string; fileName: string; filePath: string }[]>([])
const binaryFilePath = ref('')

// Splitter 尺寸
const sidebarSize = ref(parseInt(localStorage.getItem('rest.sidebarWidth') || '260'))
const requestSize = ref(parseInt(localStorage.getItem('rest.requestPercent') || '50') + '%')

function onSidebarResize(newSize: number) {
    localStorage.setItem('rest.sidebarWidth', String(Math.round(newSize)))
}

function onRequestResize(newSize: number) {
    localStorage.setItem('rest.requestPercent', String(Math.round(newSize)))
}

// SSE streaming
const sseContent = ref('')
let sseTimer: ReturnType<typeof setInterval> | null = null
const sseStopped = ref(false)

function stopStreaming() {
    if (sseTimer) {
        clearInterval(sseTimer)
        sseTimer = null
    }
    sseStopped.value = true
}

// WebSocket
const wsConnected = ref(false)
const wsConnId = ref('')
const wsMsgInput = ref('')
const wsMessages = ref<Array<{ type: string; data: string; time: number; sent: boolean }>>([])
const wsHandshakeHeaders = ref<Record<string, string>>({})
const wsLogRef = ref()
let unWsMsg: (() => void) | null = null

// curl import
const showCurlDialog = ref(false)

// ========== 请求历史（localStorage，最多保留 100 条） ==========

interface RestHistoryEntry {
    id: string
    time: number
    method: string
    url: string
    resolvedUrl: string
    status: number
    duration: number
    error?: string
    headers: KV[]
    params: KV[]
    body: string
    rawBody: string
    bodyType: 'json' | 'raw' | 'form' | 'urlencoded' | 'binary'
    formFields: FormKV[]
    urlencodedFields: FormKV[]
}

const HISTORY_KEY = 'rest.history'
const HISTORY_MAX = 100
const historyOpen = ref(false)
const history = ref<RestHistoryEntry[]>([])

function loadHistory() {
    try {
        const arr = JSON.parse(localStorage.getItem(HISTORY_KEY) || '[]')
        history.value = Array.isArray(arr) ? arr : []
    } catch {
        history.value = []
    }
}

function persistHistory() {
    try {
        localStorage.setItem(HISTORY_KEY, JSON.stringify(history.value.slice(0, HISTORY_MAX)))
    } catch {
        // 容量超限时放弃本次写入，不影响请求本身
    }
}

function recordHistory(entry: Omit<RestHistoryEntry, 'id' | 'time'>) {
    history.value.unshift({
        ...entry,
        id: Date.now().toString(36) + Math.random().toString(36).slice(2, 6),
        time: Date.now(),
    })
    if (history.value.length > HISTORY_MAX) history.value.length = HISTORY_MAX
    persistHistory()
}

function openHistory() {
    loadHistory()
    historyOpen.value = true
}

function removeHistory(i: number) {
    history.value.splice(i, 1)
    persistHistory()
}

function clearHistory() {
    history.value = []
    persistHistory()
}

function fmtHistoryTime(t: number) {
    const d = new Date(t)
    const p = (n: number) => String(n).padStart(2, '0')
    return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

async function applyHistory(h: RestHistoryEntry) {
    current.value.method = h.method
    current.value.url = h.url
    current.value.headers = JSON.parse(JSON.stringify(h.headers || []))
    current.value.params = JSON.parse(JSON.stringify(h.params || []))
    current.value.body = h.body || ''
    rawBody.value = h.rawBody || ''
    formFields.value = JSON.parse(JSON.stringify(h.formFields || []))
    urlencodedFields.value = JSON.parse(JSON.stringify(h.urlencodedFields || []))
    bodyType.value = h.bodyType || 'json'
    await nextTick()
    bodyEditorRef.value?.setContent(current.value.body)
    rawBodyEditorRef.value?.setContent(rawBody.value)
    historyOpen.value = false
    ElMessage.success('已回填到编辑器')
}

// curl generate
const codeLanguage = ref<CodeLanguage>('curl')
const generatedCurl = ref('')
const showGenCurlDialog = ref(false)

// 计算
const methodClass = computed(() => 'method-' + current.value.method.toLowerCase())

const effectiveDisplayBase = computed(() => {
    return folderEffectiveBaseUrl.value || ''
})

const statusTagType = computed(() => {
    if (!response.value) return 'info'
    const s = response.value.status
    if (s >= 200 && s < 300) return 'success'
    if (s >= 300 && s < 400) return 'warning'
    if (s >= 400) return 'danger'
    return 'info'
})

const respFilename = computed(() => {
    if (!response.value) return 'response.txt'
    const ct = (response.value.headers || {})['Content-Type'] || (response.value.headers || {})['content-type'] || ''
    if (ct.includes('json')) return 'response.json'
    if (ct.includes('xml') || ct.includes('html')) return ct.includes('html') ? 'response.html' : 'response.xml'
    if (ct.includes('javascript')) return 'response.js'
    if (ct.includes('css')) return 'response.css'
    return 'response.txt'
})

const latencyRows = computed(() => {
    if (!stressResult.value) return []
    const l = stressResult.value.latency
    return [
        { label: '延迟', min: l.min, avg: l.avg, max: l.max, p50: l.p50, p95: l.p95, p99: l.p99 },
    ]
})

const statusRows = computed(() => {
    if (!stressResult.value || !stressResult.value.statuses) return []
    const total = stressResult.value.total || 1
    const rows: { code: string; count: number; pct: string; pctNum: number }[] = []
    for (const [code, count] of Object.entries(stressResult.value.statuses)) {
        const n = (count as number) || 0
        rows.push({
            code,
            count: n,
            pct: ((n / total) * 100).toFixed(1) + '%',
            pctNum: Math.round((n / total) * 100),
        })
    }
    rows.sort((a, b) => b.count - a.count)
    return rows
})

// 压测弹窗里展示/校验的完整 URL（随请求参数实时变化）
const stressUrl = computed(() => buildURL())

watch(responseSeq, async () => {
    await nextTick()
    if (response.value?.body != null) {
        respEditorRef.value?.setContent(response.value.body)
    }
})

watch(response, () => {
    htmlPreview.value = true
})

watch(htmlPreview, async (val) => {
    if (!val) {
        await nextTick()
        respEditorRef.value?.setContent(response.value?.body ?? '')
    }
})

watch(treeFilter, (val) => {
    treeRef.value?.filter(val)
})

function filterTreeNode(value: string, data: TreeNode) {
    if (!value) return true
    return (data.name || '').toLowerCase().includes(value.toLowerCase())
}

onMounted(async () => {
    await settings.load()
    current.value.insecure = settings.restInsecureSkipVerify
    await loadRoot()
    document.addEventListener('keydown', onKeyDown)
})

onBeforeUnmount(() => {
    document.removeEventListener('keydown', onKeyDown)
    unWsMsg?.()
    unWsMsg = null
    if (wsConnId.value) {
        RestService.WSClose(wsConnId.value).catch(() => undefined)
    }
})

onDeactivated(() => {
    stopStreaming()
    unWsMsg?.()
    unWsMsg = null
    if (wsConnId.value) {
        RestService.WSClose(wsConnId.value).catch(() => { })
        wsConnId.value = ''
        wsConnected.value = false
    }
})

// ========== 树 ==========

async function loadRoot() {
    try {
        const list = await RestService.ListChildren(0)
        treeData.value = (list ?? []).map(toTreeNode)
    } catch (e: any) {
        ElMessage.error('加载请求列表失败: ' + (e?.message || e))
    }
}

async function loadNode(
    node: { level: number; data?: TreeNode },
    resolve: (data: TreeNode[]) => void,
) {
    const parentId = node.level === 0 ? 0 : (node.data?.id ?? 0)
    try {
        const list = await RestService.ListChildren(parentId)
        resolve((list ?? []).map(toTreeNode))
    } catch {
        resolve([])
    }
}

function toTreeNode(n: any): TreeNode {
    const nodeKey = (n.type || 'unknown') + '-' + n.id
    return {
        nodeKey,
        id: n.id,
        parentId: n.parentId,
        name: n.name,
        type: n.type,
        method: n.method,
        url: n.url,
        baseUrl: n.baseUrl,
        activeBaseUrl: n.activeBaseUrl,
        commonHeaders: n.commonHeaders,
        leaf: n.leaf,
        sort: n.sort,
    }
}

function selectedNode(): TreeNode | null {
    const key = treeRef.value?.getCurrentKey?.()
    if (key == null) return null
    return findNodeByKey(String(key))
}

function findNodeByKey(key: string): TreeNode | null {
    const node = treeRef.value?.getNode(key)
    return node?.data ?? null
}

function makeNodeKey(type: string, id: number): string {
    return type + '-' + id
}

async function reloadParent(parentId: number) {
    try {
        const list = await RestService.ListChildren(parentId)
        const children = (list ?? []).map(toTreeNode)
        if (parentId === 0) {
            treeData.value = children
        } else {
            const key = makeNodeKey('folder', parentId)
            treeRef.value?.updateKeyChildren(key, children)
            // 懒加载节点只替换子级时 loaded 仍为 false，之后展开会再次 load 并追加重复节点
            const node = treeRef.value?.getNode(key) as any
            if (node) node.loaded = true
        }
    } catch (e: any) {
        ElMessage.error('刷新失败: ' + (e?.message || e))
    }
}

function onNodeClick(data: TreeNode) {
    if (data.type === 'request') {
        stopStreaming()
        response.value = null
        responseSeq.value++
        loadRequest(data)
    }
}

function onNodeContext(event: MouseEvent, data: TreeNode) {
    event.preventDefault()
    event.stopPropagation()
    ctxNode.value = data
    ctxItems.value = buildCtx(data)
    openCtx(event)
}

function onTreeContext(event: MouseEvent) {
    const target = event.target as HTMLElement
    if (target.closest('.el-tree-node')) {
        return
    }
    event.preventDefault()
    onBlankContext(event)
}

function allowDrag(node: any): boolean {
    return true
}

function allowDrop(draggingNode: any, dropNode: any, type: string): boolean {
    const dragData: TreeNode = draggingNode.data
    const dropData: TreeNode = dropNode.data
    if (type === 'inner' && dropData.type !== 'folder') {
        return false
    }
    if (dragData.type === 'folder' && dropData.type === 'folder') {
        if (dragData.id === dropData.id) return false
    }
    return true
}

async function onNodeDragEnd(
    draggingNode: any,
    dropNode: any,
    dropType: string,
) {
    const dragData: TreeNode = draggingNode?.data
    // 拖到树外（dropNode 为 null）或无效落点（none）时节点并没有被移动，直接忽略。
    // 少了这一步会在读 dropNode.data 时抛错，并中断 EP 的拖拽状态清理，之后每次拖拽都会错乱。
    if (!dragData || !dropNode || dropType === 'none') return
    const dropData: TreeNode = dropNode.data
    if (!dropData) return

    let newParentId: number
    let targetId: number
    let position: string

    if (dropType === 'inner') {
        newParentId = dropData.id
        targetId = 0
        position = 'after'
    } else if (dropType === 'before') {
        newParentId = dropData.parentId
        targetId = dropData.id
        position = 'before'
    } else {
        newParentId = dropData.parentId
        targetId = dropData.id
        position = 'after'
    }

    if (dragData.type === 'folder' && newParentId === dragData.id) {
        ElMessage.warning('不能移动到自身')
        await reloadParent(dragData.parentId)
        return
    }

    try {
        await RestService.MoveNode(dragData.id, dragData.type, newParentId, targetId, position)
        await reloadParent(dragData.parentId)
        if (newParentId !== dragData.parentId) {
            await reloadParent(newParentId)
        }
    } catch (e: any) {
        ElMessage.error('移动失败: ' + (e?.message || e))
        await reloadParent(dragData.parentId)
        await reloadParent(newParentId)
    }
}

function onBlankContext(event: MouseEvent) {
    event.preventDefault()
    ctxNode.value = null
    ctxItems.value = [
        { key: 'new-folder', label: '新建文件夹', icon: Folder },
        { key: 'new-request', label: '新建请求', icon: Plus },
    ]
    if (copiedNode.value) {
        ctxItems.value.push('divider')
        ctxItems.value.push({ key: 'paste', label: '粘贴' + (copiedNode.value.name ? '「' + copiedNode.value.name + '」' : ''), icon: CopyDocument })
    }
    ctxItems.value.push('divider')
    ctxItems.value.push({ key: 'refresh', label: '刷新', icon: Refresh })
    openCtx(event)
}

function buildCtx(data: TreeNode): (CtxItem | 'divider')[] {
    const items: (CtxItem | 'divider')[] = []
    if (data.type === 'folder') {
        items.push({ key: 'new-folder', label: '新建子文件夹', icon: Folder })
        items.push({ key: 'new-request', label: '新建请求', icon: Plus })
        items.push('divider')
        items.push({ key: 'set-folder-env', label: '设置环境', icon: Setting })
        items.push('divider')
        if (copiedNode.value) {
            const canPaste = !isDescendantOf(data, copiedNode.value)
            items.push({ key: 'paste', label: '粘贴到此处', icon: CopyDocument, disabled: !canPaste,
                hint: canPaste ? '' : '不能粘贴到自己的下级' })
            items.push('divider')
        }
        items.push({ key: 'refresh', label: '刷新', icon: Refresh })
    } else {
        items.push({ key: 'new-folder-in-parent', label: '新建文件夹', icon: Folder })
        items.push({ key: 'new-request-in-parent', label: '新建请求', icon: Plus })
        items.push('divider')
        if (copiedNode.value) {
            items.push({ key: 'paste-into-parent', label: '粘贴到同级', icon: CopyDocument })
            items.push('divider')
        }
    }
    items.push({ key: 'copy', label: '复制', icon: CopyDocument })
    items.push({ key: 'rename', label: '重命名', icon: Edit })
    items.push({ key: 'delete', label: '删除', icon: Delete, danger: true })
    return items
}

function isDescendantOf(parent: TreeNode, child: TreeNode): boolean {
    if (parent.type !== 'folder') return false
    if (child.type !== 'folder') return false
    return parent.id === child.id
}

function openCtx(event: MouseEvent) {
    ctxX.value = event.clientX
    ctxY.value = event.clientY
    ctxVisible.value = false
    requestAnimationFrame(() => {
        ctxVisible.value = true
    })
}

async function onCtxPick(item: CtxItem) {
    const target = ctxNode.value
    switch (item.key) {
        case 'new-folder': {
            const parentId = target && target.type === 'folder' ? target.id : (target ? target.parentId : 0)
            await createFolder(parentId)
            break
        }
        case 'new-request': {
            const parentId = target && target.type === 'folder' ? target.id : (target ? target.parentId : 0)
            await createRequest(parentId)
            break
        }
        case 'new-folder-in-parent': {
            const parentId = target ? target.parentId : 0
            await createFolder(parentId)
            break
        }
        case 'new-request-in-parent': {
            const parentId = target ? target.parentId : 0
            await createRequest(parentId)
            break
        }
        case 'set-folder-env':
            if (target) openEnvEditor(target)
            break
        case 'refresh':
            await reloadParent(target ? target.id : 0)
            break
        case 'copy':
            if (target) {
                copiedNode.value = { ...target }
                ElMessage.success('已复制，可在目标位置右键粘贴')
            }
            break
        case 'paste': {
            if (!copiedNode.value) break
            const parentId = target && target.type === 'folder' ? target.id : 0
            if (copiedNode.value.type === 'folder' && target && target.type === 'folder' &&
                isDescendantOf(target, copiedNode.value)) {
                ElMessage.warning('不能粘贴到自己的下级')
                break
            }
            await pasteNode(parentId)
            break
        }
        case 'paste-into-parent': {
            if (!copiedNode.value) break
            const parentId = target ? target.parentId : 0
            await pasteNode(parentId)
            break
        }
        case 'rename':
            if (target) await renameNode(target)
            break
        case 'delete':
            if (target) await deleteNode(target)
            break
    }
}

async function pasteNode(parentId: number) {
    if (!copiedNode.value) return
    try {
        await RestService.CopyNode(copiedNode.value.id, copiedNode.value.type, parentId)
        await reloadParent(parentId)
        ElMessage.success('粘贴成功')
    } catch (e: any) {
        ElMessage.error('粘贴失败: ' + (e?.message || e))
    }
}

async function createFolder(parentId: number) {
    try {
        const { value } = await ElMessageBox.prompt('请输入文件夹名称', '新建文件夹', {
            confirmButtonText: '确定',
            cancelButtonText: '取消',
        })
        if (!value || !value.trim()) return
        await RestService.CreateFolder(parentId, value.trim())
        await reloadParent(parentId)
    } catch {
        // 取消
    }
}

async function createRequest(parentId: number) {
    try {
        const { value } = await ElMessageBox.prompt('请输入请求名称', '新建请求', {
            confirmButtonText: '确定',
            cancelButtonText: '取消',
        })
        if (!value || !value.trim()) return
        await RestService.CreateRequest(parentId, value.trim())
        await reloadParent(parentId)
    } catch {
        // 取消
    }
}

// VSCode 式行内重命名：树节点上直接出现输入框，Enter / 失焦提交，Esc 取消
async function renameNode(node: TreeNode) {
    renameTarget.value = node
    renamingNodeKey.value = node.nodeKey
    renameDraft.value = node.name
    await nextTick()
    renameInputRef.value?.focus()
    renameInputRef.value?.select?.()
}

function onRenameEnter(e: KeyboardEvent) {
    if (e.isComposing) return
    void commitRename()
}

function cancelRename() {
    renameTarget.value = null
    renamingNodeKey.value = ''
}

async function commitRename() {
    const node = renameTarget.value
    if (!node) return
    renameTarget.value = null
    renamingNodeKey.value = ''
    const name = renameDraft.value.trim()
    if (!name || name === node.name) return
    try {
        if (node.type === 'folder') {
            await RestService.RenameFolder(node.id, name)
        } else {
            await RestService.RenameRequest(node.id, name)
        }
        if (editingNode.value?.id === node.id && editingNode.value.type === node.type) {
            editingNode.value = { ...editingNode.value, name }
        }
        await reloadParent(node.parentId)
    } catch (e: any) {
        ElMessage.error(`重命名失败：${e?.message || e}`)
    }
}

async function deleteNode(node: TreeNode) {
    try {
        const msg = node.type === 'folder'
            ? `确定删除文件夹「${node.name}」及其全部内容？`
            : `确定删除请求「${node.name}」？`
        await ElMessageBox.confirm(msg, '删除', {
            confirmButtonText: '删除',
            cancelButtonText: '取消',
            type: 'warning',
        })
        await RestService.DeleteNode(node.id, node.type)
        if (editingNode.value && editingNode.value.id === node.id) {
            resetEditor()
        }
        await reloadParent(node.parentId)
    } catch {
        // 取消
    }
}

async function loadRequest(node: TreeNode) {
    try {
        const item = await RestService.GetRequest(node.id)
        editingNode.value = node
        editingRequestId.value = item.id
        current.value.method = item.method || 'GET'
        current.value.url = item.url || ''
        folderEffectiveBaseUrl.value = item.effectiveBaseUrl || ''
        folderCommonHeaders.value = item.folderCommonHeaders
            ? JSON.parse(JSON.stringify(item.folderCommonHeaders)).map((h: KV) => ({ enabled: true, ...h }))
            : []
        current.value.headers = item.headers
            ? JSON.parse(JSON.stringify(item.headers)).map((h: KV) => ({ enabled: true, ...h }))
            : []
        current.value.params = item.params
            ? JSON.parse(JSON.stringify(item.params)).map((p: KV) => ({ enabled: true, ...p }))
            : []
        current.value.body = item.body || ''
        rawBody.value = item.rawBody || ''
        formFields.value = item.formFields
            ? JSON.parse(JSON.stringify(item.formFields))
            : []
        urlencodedFields.value = item.urlencodedFields
            ? JSON.parse(JSON.stringify(item.urlencodedFields))
            : []
        formFiles.value = item.formFiles
            ? JSON.parse(JSON.stringify(item.formFiles))
            : []
        binaryFilePath.value = item.binaryFilePath || ''
        await nextTick()
        bodyEditorRef.value?.setContent(item.body || '')
        rawBodyEditorRef.value?.setContent(item.rawBody || '')
    } catch (e: any) {
        ElMessage.error('加载请求失败: ' + (e?.message || e))
    }
}

async function saveCurrentRequest() {
    let targetNode = editingNode.value
    let targetFolderId = targetNode?.parentId ?? 0
    let targetName = targetNode?.name

    if (!targetNode) {
        try {
            const { value } = await ElMessageBox.prompt('请输入请求名称', '保存为', {
                confirmButtonText: '保存',
                cancelButtonText: '取消',
                inputPlaceholder: '例如：获取用户信息',
            })
            if (!value || !value.trim()) return
            targetName = value.trim()
        } catch {
            return
        }
        try {
            const created = await RestService.CreateRequest(targetFolderId, targetName!)
            await reloadParent(targetFolderId)
            targetNode = findNodeByKey(makeNodeKey('request', created.id))
            if (targetNode) {
                editingNode.value = targetNode
                editingRequestId.value = created.id
                await nextTick()
                treeRef.value?.setCurrentKey(targetNode.nodeKey)
            }
        } catch (e: any) {
            ElMessage.error('创建请求失败: ' + (e?.message || e))
            return
        }
    }

    saving.value = true
    try {
        // 同步当前编辑器内容到对应的数据变量
        if (bodyType.value === 'json') {
            current.value.body = bodyEditorRef.value?.getContent() ?? current.value.body
        } else if (bodyType.value === 'raw') {
            rawBody.value = rawBodyEditorRef.value?.getContent() ?? rawBody.value
        }
        // 全部 body 类型一起保存
        await RestService.SaveRequest({
            id: editingRequestId.value,
            folderId: editingNode.value!.parentId,
            name: editingNode.value!.name,
            method: current.value.method,
            url: current.value.url,
            baseUrl: '',
            headers: current.value.headers,
            params: current.value.params,
            body: current.value.body,
            rawBody: rawBody.value,
            formFields: formFields.value,
            urlencodedFields: urlencodedFields.value,
            formFiles: formFiles.value,
            binaryFilePath: binaryFilePath.value,
        })
        ElMessage.success('保存成功')
    } catch (e: any) {
        ElMessage.error('保存失败: ' + (e?.message || e))
    } finally {
        saving.value = false
    }
}

function onKeyDown(e: KeyboardEvent) {
    const t = e.target as HTMLElement | null
    const typing = !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)
    if (e.key === 'F2' && !typing && !renamingNodeKey.value) {
        const node = treeRef.value?.getCurrentNode?.() as TreeNode | undefined
        if (node) {
            e.preventDefault()
            void renameNode(node)
            return
        }
    }
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault()
        if (editingRequestId.value) {
            saveCurrentRequest()
        }
    }
}

function resetEditor() {
    editingNode.value = null
    editingRequestId.value = 0
    folderEffectiveBaseUrl.value = ''
    folderCommonHeaders.value = []
    current.value.method = 'GET'
    current.value.url = ''
    current.value.headers = []
    current.value.params = []
    current.value.body = ''
    rawBody.value = ''
    formFields.value = []
    urlencodedFields.value = []
    formFiles.value = []
    binaryFilePath.value = ''
    bodyEditorRef.value?.setContent('')
    rawBodyEditorRef.value?.setContent('')
}

// ========== 设置环境（目录右键：基础链接 + 公共请求头） ==========

function openEnvEditor(node: TreeNode) {
    envTargetNode.value = node
    showEnvDialog.value = true
}

// 弹窗保存完成后：刷新树，并同步当前编辑请求生效的公共请求头 / 基础链接
async function onEnvSaved(node: { id: number; parentId: number }) {
    await reloadParent(node.parentId)
    if (editingNode.value) {
        try {
            const eff = await RestService.GetEffectiveCommonHeaders(editingNode.value.parentId)
            folderCommonHeaders.value = (eff || []).map((h: KV) => ({ enabled: true, ...h }))
        } catch { /* ignore */ }
        try {
            const eff = await RestService.GetEffectiveBaseURL(editingNode.value.parentId)
            folderEffectiveBaseUrl.value = eff || ''
        } catch { /* ignore */ }
    }
}

// ========== 发送请求 ==========

function isAbsoluteURL(url: string): boolean {
    return /^(https?|wss?):\/\//i.test(url.trim())
}

function buildURL(): string {
    let url = current.value.url.trim()
    if (!url) return ''

    const isWS = current.value.method === 'WS'
    const defaultProto = isWS ? 'ws://' : 'https://'

    let effectiveBase = folderEffectiveBaseUrl.value

    if (!isAbsoluteURL(url) && effectiveBase) {
        const base = effectiveBase.replace(/\/+$/, '')
        url = base + '/' + url.replace(/^\/+/, '')
        if (!isAbsoluteURL(url)) {
            url = (isWS ? 'ws://' : 'https://') + url
        }
    } else if (!isAbsoluteURL(url)) {
        url = defaultProto + url
    }

    const params = current.value.params.filter((p) => p.key && p.enabled !== false)
    if (params.length > 0) {
        const sep = url.includes('?') ? '&' : '?'
        url += sep + params.map((p) => encodeURIComponent(p.key) + '=' + encodeURIComponent(p.value)).join('&')
    }
    return url
}

function buildHeaders(): Record<string, string> {
    const h: Record<string, string> = {}

    // Layer 1: 文件夹层级合并的公共请求头
    for (const row of folderCommonHeaders.value) {
        if (row.key.trim() && row.enabled !== false) {
            h[row.key.trim()] = row.value
        }
    }

    // Layer 2: 请求自身的 headers（最高优先级，覆盖以上所有）
    for (const row of current.value.headers) {
        if (row.key.trim() && row.enabled !== false) {
            h[row.key.trim()] = row.value
        }
    }
    return h
}

function toggleInsecure() {
    current.value.insecure = !current.value.insecure
    settings.set('rest.insecureSkipVerify', current.value.insecure ? '1' : '0')
}

async function send() {
    const url = buildURL()
    if (!url) {
        ElMessage.warning('请输入 URL')
        return
    }

    let body = current.value.body
    if (bodyType.value === 'json') {
        body = bodyEditorRef.value?.getContent() ?? current.value.body
        current.value.body = body
    } else if (bodyType.value === 'raw') {
        body = rawBodyEditorRef.value?.getContent() ?? rawBody.value
    }

    const headers = buildHeaders()

    let reqBody = ''
    if (bodyType.value === 'json') {
        reqBody = stripJsonComments(body)
    } else if (bodyType.value === 'raw') {
        reqBody = body
    } else if (bodyType.value === 'urlencoded') {
        reqBody = urlencodedFields.value
            .filter((f) => f.key && f.enabled !== false)
            .map((f) => encodeURIComponent(f.key) + '=' + encodeURIComponent(f.value))
            .join('&')
        if (!headers['Content-Type'] && !headers['content-type']) {
            headers['Content-Type'] = 'application/x-www-form-urlencoded'
        }
    }

    const req: RestRequest = {
        method: current.value.method,
        url,
        headers,
        body: reqBody,
        timeout: 30,
        insecure: current.value.insecure,
        formFiles: bodyType.value === 'form' ? formFiles.value.filter(f => f.filePath).map(f => ({
            fieldName: f.fieldName,
            fileName: f.fileName,
            filePath: f.filePath,
        })) : [],
        formData: bodyType.value === 'form'
            ? Object.fromEntries(formFields.value.filter(f => f.key && f.enabled !== false).map(f => [f.key, f.value]))
            : {},
    }

    sending.value = true
    response.value = null

    const snapshot: Omit<RestHistoryEntry, 'id' | 'time' | 'status' | 'duration'> = {
        method: current.value.method,
        url: current.value.url,
        resolvedUrl: url,
        headers: JSON.parse(JSON.stringify(current.value.headers)),
        params: JSON.parse(JSON.stringify(current.value.params)),
        body: current.value.body,
        rawBody: bodyType.value === 'raw' ? body : rawBody.value,
        bodyType: bodyType.value,
        formFields: JSON.parse(JSON.stringify(formFields.value)),
        urlencodedFields: JSON.parse(JSON.stringify(urlencodedFields.value)),
    }

    try {
        const resp = await RestService.Send(req)
        recordHistory({ ...snapshot, status: resp.status, duration: resp.duration, error: resp.error || undefined })
        response.value = resp
        responseSeq.value++
        resTab.value = 'body'
        if (resp.error) {
            ElMessage.warning(resp.error)
        }
        if (resp.streaming && resp.streamId) {
            if (sseTimer) clearInterval(sseTimer)
            sseStopped.value = false
            sseTimer = setInterval(async () => {
                try {
                    const chunk = await RestService.ReadStreamChunks(resp.streamId!)
                    if (chunk.data) {
                        response.value!.body += chunk.data
                        responseSeq.value++
                    }
                    if (chunk.done) {
                        stopStreaming()
                    }
                } catch { }
            }, 150)
        }
    } catch (e: any) {
        recordHistory({ ...snapshot, status: 0, duration: 0, error: e?.message || String(e) })
        ElMessage.error({ message: '请求失败: ' + (e?.message || String(e)), duration: 2000, showClose: true })
    } finally {
        sending.value = false
    }
}

function formatSize(bytes: number): string {
    if (bytes < 1024) return bytes + ' B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

function selectFormFile(index: number) {
    const input = document.createElement('input')
    input.type = 'file'
    input.multiple = true
    input.onchange = () => {
        const files = input.files
        if (!files || !files.length) return
        const first = files[0]
        formFiles.value[index].fileName = first.name
        formFiles.value[index].filePath = (first as any).path || first.name
        for (let i = 1; i < files.length; i++) {
            const f = files[i]
            formFiles.value.push({
                fieldName: 'file',
                fileName: f.name,
                filePath: (f as any).path || f.name,
            })
        }
    }
    input.click()
}

function selectFormFiles() {
    const input = document.createElement('input')
    input.type = 'file'
    input.multiple = true
    input.onchange = () => {
        const files = input.files
        if (!files || !files.length) return
        for (let i = 0; i < files.length; i++) {
            const f = files[i]
            formFiles.value.push({
                fieldName: 'file',
                fileName: f.name,
                filePath: (f as any).path || f.name,
            })
        }
    }
    input.click()
}

function selectBinaryFile() {
    const input = document.createElement('input')
    input.type = 'file'
    input.onchange = () => {
        const file = input.files?.[0]
        if (file) {
            binaryFilePath.value = (file as any).path || file.name
            current.value.body = binaryFilePath.value
        }
    }
    input.click()
}

// ========== curl 导入/生成 ==========

function openCurlImport() {
    showCurlDialog.value = true
}

// 弹窗解析完成后，把解析结果套用到当前请求
async function applyCurlImport(parsed: CurlParseResult) {
    try {
        if (parsed.method) current.value.method = parsed.method
        if (parsed.url) current.value.url = parsed.url
        if (parsed.headers.length) {
            current.value.headers = parsed.headers.map((h: KV) => ({ ...h, enabled: true }))
        }
        if (parsed.params.length) {
            current.value.params = parsed.params.map((p: KV) => ({ ...p, enabled: true }))
        }

        const contentType =
            parsed.headers.find((h) => h.key.toLowerCase() === 'content-type')?.value.toLowerCase() || ''
        const body = parsed.body

        // 按 curl 的表单 / 编码 / JSON / 原始文本选择合适的 Body 类型
        if (parsed.formParts.length) {
            formFields.value = []
            formFiles.value = []
            for (const part of parsed.formParts) {
                const idx = part.indexOf('=')
                if (idx <= 0) continue
                const key = part.slice(0, idx)
                const value = part.slice(idx + 1)
                if (value.startsWith('@')) {
                    const filePath = value.slice(1)
                    const fileName = filePath.split(/[\\/]/).pop() || filePath
                    formFiles.value.push({ fieldName: key, fileName, filePath })
                } else {
                    formFields.value.push({ key, value, enabled: true })
                }
            }
            bodyType.value = 'form'
        } else if (parsed.urlencodedParts.length || (body && contentType.includes('application/x-www-form-urlencoded'))) {
            const pairs = parsed.urlencodedParts.length ? parsed.urlencodedParts : body.split('&')
            urlencodedFields.value = pairs
                .map((pair) => {
                    const idx = pair.indexOf('=')
                    const key = idx >= 0 ? pair.slice(0, idx) : pair
                    const value = idx >= 0 ? pair.slice(idx + 1) : ''
                    return {
                        key: safeDecode(key),
                        value: safeDecode(value),
                        enabled: true,
                    }
                })
                .filter((f) => f.key)
            bodyType.value = 'urlencoded'
            current.value.body = body
        } else if (body) {
            const isJson = contentType.includes('json') || /^\s*[[{]/.test(body)
            bodyType.value = isJson ? 'json' : 'raw'
            current.value.body = body
            rawBody.value = body
        }

        reqTab.value = 'body'
        showCurlDialog.value = false
        ElMessage.success('curl 导入成功')

        // 用户当前已经停留在 JSON 标签时 watch(bodyType) 不会触发，
        // 必须显式把解析出的 Body 写进编辑器，否则界面上看不到导入内容。
        await nextTick()
        if (bodyType.value === 'json') {
            bodyEditorRef.value?.setContent(current.value.body)
        } else if (bodyType.value === 'raw') {
            rawBodyEditorRef.value?.setContent(rawBody.value)
        }
    } catch (e: any) {
        ElMessage.error('导入失败: ' + (e?.message || e))
    }
}

// ========== 代码生成 ==========

function currentCode(): string {
    return buildCode(codeLanguage.value, {
        url: buildURL(),
        method: current.value.method.toUpperCase(),
        headers: buildHeaders(),
        body: getCleanBody(),
    })
}

function genCode() {
    if (!buildURL()) {
        ElMessage.warning('请输入 URL')
        return
    }
    generatedCurl.value = currentCode()
    showGenCurlDialog.value = true
}

function getCleanBody(): string {
    let body = current.value.body || ''
    if (bodyType.value === 'json') {
        body = bodyEditorRef.value?.getContent() ?? current.value.body
        let cleaned = stripJsonComments(body)
        try {
            cleaned = JSON.stringify(JSON.parse(cleaned))
        } catch {
            /* 解析失败则保留清理后的原文 */
        }
        return cleaned
    }
    if (bodyType.value === 'raw') {
        return rawBodyEditorRef.value?.getContent() ?? rawBody.value
    }
    return body
}

watch(codeLanguage, () => {
    generatedCurl.value = currentCode()
})

// ========== WebSocket ==========

async function wsDoConnect() {
    const url = buildURL()
    if (!url) { ElMessage.warning('请输入 WebSocket URL'); return }
    const headers = buildHeaders()
    try {
        const result = await RestService.WSConnect({ url, headers })
        if (result.error) { ElMessage.error(result.error); return }
        wsConnId.value = result.connId
        wsConnected.value = true
        wsMessages.value = []
        wsHandshakeHeaders.value = (result.headers || {}) as Record<string, string>
        resTab.value = 'body'

        unWsMsg?.()
        unWsMsg = Events.On(EVENTS.restWsMessage, (evt: any) => {
            const msg = evt.data
            if (msg && msg.connId === wsConnId.value) {
                wsMessages.value.push({
                    type: msg.type,
                    data: msg.data,
                    time: msg.time,
                    sent: msg.sent,
                })
                if (wsMessages.value.length > 500) {
                    wsMessages.value.splice(0, wsMessages.value.length - 500)
                }
                if (msg.type === 'close') {
                    wsConnected.value = false
                    wsConnId.value = ''
                }
            }
        })
    } catch (e: any) {
        ElMessage.error('连接失败: ' + (e?.message || String(e)))
    }
}

async function wsDoSend() {
    if (!wsMsgInput.value.trim() || !wsConnected.value) return
    try {
        await RestService.WSSend(wsConnId.value, wsMsgInput.value.trim())
        wsMsgInput.value = ''
    } catch (e: any) {
        ElMessage.error('发送失败: ' + (e?.message || String(e)))
    }
}

async function wsDoClose() {
    unWsMsg?.()
    unWsMsg = null
    if (wsConnId.value) {
        try { await RestService.WSClose(wsConnId.value) } catch { }
    }
    wsConnected.value = false
    wsConnId.value = ''
}

// ========== 压力测试 ==========

async function runStress(form: { concurrency: number; total: number; timeout: number }) {
    const url = buildURL()
    if (!url) {
        ElMessage.warning('请输入 URL')
        return
    }

    let body = current.value.body
    if (bodyType.value === 'json') {
        body = bodyEditorRef.value?.getContent() ?? current.value.body
        current.value.body = body
    } else if (bodyType.value === 'raw') {
        body = rawBodyEditorRef.value?.getContent() ?? rawBody.value
    }

    stressResult.value = null
    stressProgress.value = { current: 0, total: form.total }
    stressRunning.value = true

    stressTimer = setInterval(() => {
        if (stressProgress.value.current < form.total) {
            stressProgress.value.current = Math.min(
                stressProgress.value.current + form.concurrency * 2,
                form.total,
            )
        }
    }, 300)

    try {
        const req: StressTestRequest = {
            method: current.value.method,
            url,
            headers: buildHeaders(),
            body: body || '',
            timeout: form.timeout,
            concurrency: form.concurrency,
            total: form.total,
            insecure: current.value.insecure,
        }
        const result = await RestService.StressTest(req)
        stressResult.value = result
        stressProgress.value.current = form.total
        if (result.failure === 0) {
            ElMessage.success(`压测完成: ${result.total} 次全部成功, QPS ${result.qps}`)
        } else {
            ElMessage.warning(`压测完成: ${result.success} 成功 / ${result.failure} 失败, QPS ${result.qps}`)
        }
    } catch (e: any) {
        ElMessage.error('压测失败: ' + (e?.message || String(e)))
    } finally {
        stopStress()
    }
}

function stopStress() {
    stressRunning.value = false
    if (stressTimer) {
        clearInterval(stressTimer)
        stressTimer = null
    }
}
</script>

<style scoped>
.rest-view {
    display: flex;
    height: 100vh;
    overflow: hidden;
}

.root-splitter,
.body-splitter {
    height: 100%;
}

.root-splitter :deep(.el-splitter-panel),
.body-splitter :deep(.el-splitter-panel) {
    min-width: 0;
    min-height: 0;
    overflow: hidden;
}

.root-splitter :deep(.el-splitter-bar__dragger:before),
.body-splitter :deep(.el-splitter-bar__dragger:before) {
    background-color: var(--border-color);
}

.root-splitter :deep(.el-splitter-bar:hover .el-splitter-bar__dragger:before),
.body-splitter :deep(.el-splitter-bar:hover .el-splitter-bar__dragger:before) {
    background-color: var(--el-color-primary);
}

.root-splitter :deep(.el-splitter-bar__dragger:hover),
.body-splitter :deep(.el-splitter-bar__dragger:hover) {
    background: transparent;
}

.rest-sidebar {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--bg-secondary);
    overflow: hidden;
}

.sidebar-search {
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.sidebar-tree {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 8px 0;
}

.sidebar-tree :deep(.el-tree-node__content) {
    height: 32px;
    padding-right: 8px;
}

.tree-node {
    display: flex;
    align-items: center;
    gap: 6px;
    overflow: hidden;
    flex: 1;
}

.tree-method {
    font-size: 10px;
    font-weight: 700;
    padding: 1px 5px;
    border-radius: 3px;
    flex-shrink: 0;
    text-transform: uppercase;
}

.rest-rename-input {
    width: 160px;
    max-width: 100%;
}

.rest-rename-input :deep(.el-input__wrapper) {
    padding: 0 4px;
    height: 22px;
}

.tree-method.method-get {
    color: var(--success-color);
}

.tree-method.method-post {
    color: var(--warning-color);
}

.tree-method.method-put {
    color: #e6a23c;
}

.tree-method.method-delete {
    color: var(--danger-color);
}

.tree-method.method-patch {
    color: #e6a23c;
}

.tree-method.method-head {
    color: var(--text-secondary);
}

.tree-method.method-options {
    color: var(--text-secondary);
}

.tree-node-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
}

.tree-base-url-tag {
    font-size: 10px;
    padding: 0 4px;
    background: var(--success-color);
    color: #fff;
    border-radius: 3px;
    flex-shrink: 0;
    max-width: 60px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.rest-main {
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
    height: 100%;
}

.rest-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border-color);
    background: var(--bg-secondary);
    flex-shrink: 0;
}

.method-select {
    width: 110px;
    flex-shrink: 0;
}

.method-select :deep(.el-input__wrapper) {
    font-weight: 700;
    letter-spacing: 0.5px;
}

.method-select.method-get :deep(.el-input__wrapper) {
    color: var(--success-color);
}

.method-select.method-post :deep(.el-input__wrapper) {
    color: var(--warning-color);
}

.method-select.method-put :deep(.el-input__wrapper) {
    color: #e6a23c;
}

.method-select.method-delete :deep(.el-input__wrapper) {
    color: var(--danger-color);
}

.method-select.method-patch :deep(.el-input__wrapper) {
    color: #e6a23c;
}

.method-select.method-head :deep(.el-input__wrapper) {
    color: var(--text-secondary);
}

.method-select.method-options :deep(.el-input__wrapper) {
    color: var(--text-secondary);
}

.url-input {
    flex: 1;
}

.env-hint {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 16px;
    background: var(--bg-tertiary);
    border-bottom: 1px solid var(--border-color);
    font-size: 12px;
    color: var(--text-secondary);
    flex-shrink: 0;
}

.rest-request {
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    height: 100%;
}

.rest-response {
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    height: 100%;
}

.response-empty-hint {
    text-align: center;
    color: var(--text-secondary);
}

.response-empty-hint p {
    margin-top: 8px;
    font-size: 14px;
}

.rest-tabs {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-height: 0;
}

.rest-tabs :deep(.el-tabs__header) {
    margin: 0;
    padding: 0 12px;
    flex-shrink: 0;
}

.rest-tabs :deep(.el-tabs__content) {
    flex: 1;
    overflow: hidden;
    min-height: 0;
}

.rest-tabs :deep(.el-tab-pane) {
    height: 100%;
    overflow: hidden;
}

.response-status-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 16px;
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.response-duration {
    font-size: 13px;
    color: var(--text-secondary);
}

.response-size {
    font-size: 13px;
    color: var(--text-secondary);
}

.kv-editor {
    padding: 12px 16px;
    overflow-y: auto;
    height: 100%;
    box-sizing: border-box;
}

.kv-editor.readonly {
    overflow-y: auto;
}

.kv-row {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 6px;
}

.kv-check {
    flex-shrink: 0;
    margin-right: 2px;
}

.kv-key {
    width: 200px;
    flex-shrink: 0;
}

.kv-val {
    flex: 1;
}

.kv-del {
    flex-shrink: 0;
}

.kv-key-ro {
    width: 200px;
    flex-shrink: 0;
    font-weight: 600;
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.kv-val-ro {
    flex: 1;
    font-size: 12px;
    word-break: break-all;
}

.kv-empty {
    color: var(--text-secondary);
    font-size: 13px;
}

.code-wrap {
    height: 100%;
    overflow: hidden;
}

.html-toggle {
    margin-left: auto;
}

.html-preview-wrap {
    height: 100%;
    overflow: auto;
    background: #fff;
}

.html-preview-iframe {
    width: 100%;
    height: 100%;
    border: none;
}

.body-type-bar {
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.form-data-editor {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
}

.form-files {
    padding: 8px 16px;
    border-top: 1px solid var(--border-color);
}

.form-file-row {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 6px;
}

.form-file-name {
    font-size: 12px;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 200px;
}

.binary-editor {
    padding: 16px;
    display: flex;
    align-items: center;
    gap: 12px;
}

.baseurl-node-name {
    font-weight: 600;
    color: var(--text-primary);
}

.baseurl-hint {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--text-tertiary);
}

/* 设置环境弹窗 / 压力测试 / curl 生成弹窗的样式已随组件拆分到 components/rest/ */

/* WebSocket */
.ws-panel {
    display: flex;
    flex-direction: column;
    gap: 10px;
    height: 100%;
    min-height: 0;
}

.ws-input-bar {
    display: flex;
    gap: 8px;
}

.ws-input-bar .el-input {
    flex: 1;
}

.ws-log {
    flex: 1;
    min-height: 120px;
    overflow-y: auto;
    background: var(--bg-tertiary);
    border-radius: 6px;
    padding: 10px;
    font-family: var(--code-font);
    font-size: 13px;
}

.ws-empty {
    text-align: center;
    color: var(--text-tertiary);
    padding: 40px 0;
}

.ws-msg {
    margin-bottom: 10px;
    padding: 6px 10px;
    border-radius: 4px;
    background: var(--bg-secondary);
    border-left: 3px solid var(--border-color);
}

.ws-msg.sent {
    border-left-color: var(--success-color, #67c23a);
    background: rgba(103, 194, 58, 0.08);
}

.ws-msg.close {
    border-left-color: var(--danger-color, #f56c6c);
    background: rgba(245, 108, 108, 0.08);
}

.ws-ts {
    font-size: 11px;
    color: var(--text-tertiary);
    margin-right: 8px;
}

.ws-dir {
    display: inline-block;
    min-width: 20px;
    font-weight: bold;
    color: var(--text-secondary);
    margin-right: 6px;
}

.ws-type {
    font-weight: 600;
    color: var(--danger-color, #f56c6c);
}

.ws-data {
    margin: 4px 0 0 0;
    white-space: pre-wrap;
    word-break: break-all;
    color: var(--text-primary);
}

.ws-send-bar {
    display: flex;
    gap: 8px;
    align-items: flex-end;
}

.ws-send-bar .el-textarea {
    flex: 1;
}

.toolbar-more {
    margin-left: auto;
}

/* 请求历史抽屉 */
.history-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border-color);
    margin-bottom: 8px;
}

.history-count {
    font-size: 12px;
    color: var(--text-secondary);
}

.history-empty {
    padding: 48px 0;
    text-align: center;
    font-size: 13px;
    color: var(--text-muted);
}

.history-item {
    padding: 8px 10px;
    border-radius: 6px;
    cursor: pointer;
    border: 1px solid transparent;
}

.history-item:hover {
    background: var(--tab-hover-2);
    border-color: var(--border-color);
}

.history-line1 {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}

.history-url {
    flex: 1;
    min-width: 0;
    font-size: 12.5px;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.history-del {
    flex-shrink: 0;
    color: var(--text-muted);
    cursor: pointer;
    visibility: hidden;
}

.history-item:hover .history-del {
    visibility: visible;
}

.history-del:hover {
    color: var(--danger-color);
}

.history-line2 {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 4px;
    padding-left: 2px;
    font-size: 11.5px;
    color: var(--text-muted);
}

.history-status {
    font-weight: 600;
}

.history-status.ok {
    color: var(--success-color);
}

.history-status.warn {
    color: var(--warning-color);
}

.history-status.err {
    color: var(--danger-color);
}
</style>