<template>
  <div class="rest-view">
    <el-splitter :layout="'horizontal'" class="root-splitter">
      <el-splitter-panel :size="sidebarSize" :min="180" :max="520" @resize="onSidebarResize">
        <div class="rest-sidebar">
          <div class="sidebar-search">
            <el-input
              v-model="treeFilter"
              size="small"
              placeholder="搜索请求..."
              clearable
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
          </div>

          <div class="sidebar-tree" @contextmenu.prevent="onBlankContext">
            <el-tree
              ref="treeRef"
              :data="treeData"
              node-key="id"
              :props="{ label: 'name', children: 'children', isLeaf: 'leaf' }"
              highlight-current
              :expand-on-click-node="true"
              lazy
              :load="loadNode"
              :filter-node-method="filterTreeNode"
              @node-click="onNodeClick"
              @node-contextmenu="onNodeContext"
              empty-text="暂无请求，右键新建"
            >
              <template #default="{ data }">
                <span class="tree-node">
                  <el-icon :color="data.type === 'folder' ? '#e6c06c' : ''">
                    <Folder v-if="data.type === 'folder'" />
                    <Promotion v-else />
                  </el-icon>
                  <span
                    v-if="data.type === 'request'"
                    class="tree-method"
                    :class="'method-' + (data.method || 'get').toLowerCase()"
                  >{{ data.method }}</span>
                  <span class="tree-node-name" :title="data.name">{{ data.name }}</span>
                  <span v-if="data.type === 'folder' && data.activeBaseUrl" class="tree-base-url-tag">{{ data.activeBaseUrl }}</span>
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
          <el-option
            v-for="m in methods"
            :key="m"
            :label="m"
            :value="m"
          />
        </el-select>
        <el-input
          v-model="current.url"
          class="url-input"
          :placeholder="current.method === 'WS' ? 'ws://localhost:8080/ws' : '输入请求 URL，例如 /api/users'"
          clearable
          :disabled="current.method === 'WS' && wsConnected"
          @keydown.enter="send"
        />
        <el-button v-if="current.method !== 'WS'" type="primary" :loading="sending" @click="send">
          <el-icon v-if="!sending"><Promotion /></el-icon>
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
            <el-icon><MoreFilled /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="openCurlImport">
                <el-icon><Download /></el-icon>导入 curl
              </el-dropdown-item>
              <el-dropdown-item @click="genCode">
                <el-icon><CopyDocument /></el-icon>生成代码
              </el-dropdown-item>
              <el-dropdown-item divided @click="showStressDialog = true">
                <el-icon><TrendCharts /></el-icon>压力测试
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>

      <!-- 环境提示 -->
      <div class="env-hint" v-if="effectiveDisplayBase && current.url && !isAbsoluteURL(current.url)">
        <el-icon><Connection /></el-icon>
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
                  <el-input
                    v-model="row.key"
                    class="kv-key"
                    placeholder="Header 名"
                  />
                  <el-input
                    v-model="row.value"
                    class="kv-val"
                    placeholder="Header 值"
                  />
                  <el-button
                    size="small"
                    :icon="Delete"
                    circle
                    class="kv-del"
                    @click="current.headers.splice(i, 1)"
                  />
                </div>
                <el-button size="small" :icon="Plus" @click="current.headers.push({ key: '', value: '', enabled: true })">
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
                  <el-button
                    size="small"
                    :icon="Delete"
                    circle
                    class="kv-del"
                    @click="current.params.splice(i, 1)"
                  />
                </div>
                <el-button size="small" :icon="Plus" @click="current.params.push({ key: '', value: '', enabled: true })">
                  添加参数
                </el-button>
              </div>
            </el-tab-pane>
            <el-tab-pane label="Body" name="body">
              <div class="body-type-bar">
                <el-radio-group v-model="bodyType" size="small">
                  <el-radio-button value="json">JSON</el-radio-button>
                  <el-radio-button value="form">Form-Data</el-radio-button>
                  <el-radio-button value="urlencoded">Urlencoded</el-radio-button>
                  <el-radio-button value="binary">Binary</el-radio-button>
                </el-radio-group>
              </div>
              <div class="code-wrap" v-if="bodyType === 'json'">
                <CodeEditor
                  ref="bodyEditorRef"
                  filename="request.json"
                  wrap
                />
              </div>
              <div class="kv-editor" v-else-if="bodyType === 'urlencoded'">
                <div class="kv-row" v-for="(row, i) in urlencodedFields" :key="'u'+i">
                  <el-checkbox v-model="row.enabled" class="kv-check" />
                  <el-input v-model="row.key" class="kv-key" placeholder="参数名" />
                  <el-input v-model="row.value" class="kv-val" placeholder="参数值" />
                  <el-button size="small" :icon="Delete" circle @click="urlencodedFields.splice(i, 1)" />
                </div>
                <el-button size="small" :icon="Plus" @click="urlencodedFields.push({ key: '', value: '', enabled: true })">添加参数</el-button>
              </div>
              <div class="form-data-editor" v-else-if="bodyType === 'form'">
                <div class="kv-editor" style="height:auto;max-height:50%">
                  <div class="kv-row" v-for="(row, i) in formFields" :key="'f'+i">
                    <el-checkbox v-model="row.enabled" class="kv-check" />
                    <el-input v-model="row.key" class="kv-key" placeholder="字段名" />
                    <el-input v-model="row.value" class="kv-val" placeholder="值" />
                    <el-button size="small" :icon="Delete" circle @click="formFields.splice(i,1)" />
                  </div>
                  <el-button size="small" :icon="Plus" @click="formFields.push({key:'',value:'',enabled:true})">添加字段</el-button>
                </div>
                <div class="form-files">
                  <div class="form-file-row" v-for="(f, i) in formFiles" :key="'file'+i">
                    <el-input v-model="f.fieldName" class="kv-key" placeholder="字段名" />
                    <el-button size="small" @click="selectFormFile(i)">{{ f.fileName || '选择文件' }}</el-button>
                    <span class="form-file-name" v-if="f.fileName">{{ f.fileName }}</span>
                    <el-button size="small" :icon="Delete" circle @click="formFiles.splice(i,1)" />
                  </div>
                  <el-button size="small" :icon="Plus" @click="formFiles.push({fieldName:'file',fileName:'',filePath:''})">添加文件</el-button>
                  <el-button size="small" @click="selectFormFiles">批量选择</el-button>
                </div>
              </div>
              <div class="binary-editor" v-else-if="bodyType === 'binary'">
                <el-button size="small" @click="selectBinaryFile">选择文件</el-button>
                <span class="form-file-name" v-if="binaryFilePath">{{ binaryFilePath }}</span>
              </div>
            </el-tab-pane>
          </el-tabs>
          </div>
        </el-splitter-panel>

        <el-splitter-panel :min="120">
          <div class="rest-response">
          <div class="response-status-bar" v-if="response">
            <el-tag
              :type="statusTagType"
              size="large"
              effect="dark"
            >
              {{ response.status }} {{ response.statusText }}
            </el-tag>
            <span class="response-duration">{{ response.duration }} ms</span>
            <span class="response-size">{{ formatSize(response.body?.length || 0) }}</span>
            <el-button
              v-if="isHtmlResponse"
              size="small"
              text
              class="html-toggle"
              @click="htmlPreview = !htmlPreview"
            >
              {{ htmlPreview ? '查看源码' : '页面预览' }}
            </el-button>
          </div>
          <el-tabs v-model="resTab" class="rest-tabs">
            <el-tab-pane label="Body" name="body">
              <template v-if="current.method === 'WS'">
                <template v-if="wsConnected">
                  <div class="ws-log" ref="wsLogRef">
                    <div v-if="!wsMessages.length" class="ws-empty">暂无消息</div>
                    <div v-for="(m, i) in wsMessages" :key="i" class="ws-msg" :class="{ sent: m.sent, close: m.type === 'close' }">
                      <span class="ws-ts">{{ new Date(m.time).toLocaleTimeString() }}</span>
                      <span class="ws-dir">{{ m.sent ? '>>' : (m.type === 'close' ? '×' : '<<') }}</span>
                      <span class="ws-type" v-if="m.type === 'close'">连接关闭</span>
                      <pre class="ws-data">{{ m.data }}</pre>
                    </div>
                  </div>
                  <div class="ws-send-bar">
                    <el-input v-model="wsMsgInput" type="textarea" :autosize="{ minRows: 1, maxRows: 4 }"
                      @keydown.enter.exact.prevent="wsDoSend" placeholder="输入消息，Enter 发送" />
                    <el-button type="primary" @click="wsDoSend" :disabled="!wsMsgInput.trim()">发送</el-button>
                    <el-button size="small" text @click="wsMessages = []">清空</el-button>
                  </div>
                </template>
                <div v-else class="response-empty-hint">
                  <el-icon :size="36" color="var(--border-strong)"><Connection /></el-icon>
                  <p>点击发送按钮建立 WebSocket 连接</p>
                </div>
              </template>
              <template v-else-if="response">
                <div class="html-preview-wrap" v-if="isHtmlResponse && htmlPreview">
                  <iframe
                    class="html-preview-iframe"
                    :srcdoc="response.body"
                    sandbox="allow-same-origin allow-scripts"
                  />
                </div>
                <div class="code-wrap" v-else>
                  <CodeEditor
                    ref="respEditorRef"
                    :key="'resp-' + responseSeq"
                    :filename="respFilename"
                    wrap
                  />
                </div>
              </template>
              <div v-else class="response-empty-hint">
                <el-icon :size="36" color="var(--border-strong)"><Promotion /></el-icon>
                <p>{{ sending ? '请求中...' : '点击发送按钮发起请求' }}</p>
              </div>
            </el-tab-pane>
            <el-tab-pane label="Headers" name="resHeaders">
              <template v-if="current.method === 'WS'">
                <div class="kv-editor readonly">
                  <div class="kv-row" v-for="(val, key) in (wsHandshakeHeaders || {})" :key="key">
                    <span class="kv-key-ro">{{ key }}</span>
                    <span class="kv-val-ro">{{ val }}</span>
                  </div>
                  <p v-if="!Object.keys(wsHandshakeHeaders || {}).length" class="kv-empty">无握手响应头</p>
                </div>
              </template>
              <template v-else-if="response">
                <div class="kv-editor readonly">
                  <div class="kv-row" v-for="(val, key) in (response.headers || {})" :key="key">
                    <span class="kv-key-ro">{{ key }}</span>
                    <span class="kv-val-ro">{{ val }}</span>
                  </div>
                  <p v-if="!Object.keys(response.headers || {}).length" class="kv-empty">无响应头</p>
                </div>
              </template>
              <div v-else class="response-empty-hint">
                <el-icon :size="36" color="var(--border-strong)"><Promotion /></el-icon>
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

    <!-- 设置环境弹窗（目录右键：基础链接 + 公共请求头，同一页面） -->
    <el-dialog v-model="showEnvDialog" :title="'设置环境 - ' + (envTargetNode?.name || '')" width="750px" destroy-on-close>
      <!-- 基础链接区域 -->
      <el-divider content-position="left">
        <el-icon><Connection /></el-icon>
        <span style="margin-left:4px">基础链接</span>
      </el-divider>
      <p class="env-tab-hint">
        <el-icon><InfoFilled /></el-icon>
        可配置多个基础链接（如正式/测试），标记一个为「当前使用」。为空则继承上级目录。
      </p>
      <div class="env-base-url-list">
        <div class="env-base-item" v-for="(item, i) in folderEnvList" :key="item.id || ('new_'+i)">
          <el-input v-model="item.name" class="env-base-name" placeholder="名称（如：正式、测试）" size="small" />
          <el-input v-model="item.baseUrl" class="env-base-url" placeholder="https://api.example.com" size="small" />
          <el-tag v-if="item.isActive" size="small" type="success" class="env-active-tag">当前使用</el-tag>
          <el-button v-else size="small" text type="primary" @click="setActiveEnvItem(item, i)">启用</el-button>
          <el-button size="small" text type="danger" :icon="Delete" @click="removeEnvItem(i)" />
        </div>
        <el-button size="small" :icon="Plus" @click="addEnvItem">添加基础链接</el-button>
      </div>

      <!-- 公共请求头区域 -->
      <el-divider content-position="left" style="margin-top:20px">
        <el-icon><Setting /></el-icon>
        <span style="margin-left:4px">公共请求头</span>
      </el-divider>
      <p class="env-tab-hint">
        <el-icon><InfoFilled /></el-icon>
        文件夹级公共请求头，所有子请求自动携带。优先级：请求自身 Headers > 文件夹公共请求头。同 key 由高优先级覆盖。
      </p>
      <div class="kv-editor" style="height: auto; max-height: 300px; border: 1px solid var(--border-color); border-radius: 6px;">
        <div class="kv-row" v-for="(row, i) in folderHeadersForm" :key="i">
          <el-input v-model="row.key" class="kv-key" placeholder="Header 名" size="small" />
          <el-input v-model="row.value" class="kv-val" placeholder="Header 值" size="small" />
          <el-button size="small" :icon="Delete" circle @click="folderHeadersForm.splice(i, 1)" />
        </div>
        <el-button size="small" :icon="Plus" @click="folderHeadersForm.push({ key: '', value: '', enabled: true })">
          添加请求头
        </el-button>
      </div>
      <template #footer>
        <el-button @click="showEnvDialog = false">取消</el-button>
        <el-button type="primary" @click="saveEnvSettings" :loading="savingEnv">保存</el-button>
      </template>
    </el-dialog>

    <!-- curl 导入弹窗 -->
    <el-dialog v-model="showCurlDialog" title="导入 curl" width="650px" destroy-on-close>
      <el-input
        v-model="curlText"
        type="textarea"
        :rows="10"
        placeholder="粘贴 curl 命令，例如：&#10;curl -X POST https://api.example.com/users \&#10;  -H 'Content-Type: application/json' \&#10;  -d '{&quot;name&quot;: &quot;test&quot;}'"
      />
      <template #footer>
        <el-button @click="showCurlDialog = false">取消</el-button>
        <el-button type="primary" @click="importCurl">解析并导入</el-button>
      </template>
    </el-dialog>

    <!-- curl 生成弹窗 -->
    <el-dialog v-model="showGenCurlDialog" :title="genDialogTitle" width="750px" destroy-on-close>
      <div class="curl-gen-toolbar">
        <span class="curl-gen-label">语言：</span>
        <el-select v-model="codeLanguage" size="small" class="curl-gen-select">
          <el-option label="cURL（Bash/Linux）" value="curl" />
          <el-option label="cURL（CMD）" value="curl-cmd" />
          <el-option label="cURL（PowerShell）" value="curl-ps" />
          <el-option label="cURL（单行）" value="curl-single" />
          <el-option label="Python（requests）" value="python" />
          <el-option label="JavaScript（fetch）" value="javascript" />
          <el-option label="Go（net/http）" value="go" />
        </el-select>
        <el-button
          size="small"
          class="curl-gen-copy"
          :icon="CopyDocument"
          @click="copyGeneratedCurl"
        >复制</el-button>
      </div>
      <div class="curl-gen-editor">
        <CodeEditor
          ref="curlEditorRef"
          :filename="genEditorFilename"
          wrap
        />
      </div>
    </el-dialog>

    <!-- 压力测试弹窗 -->
    <el-dialog v-model="showStressDialog" title="压力测试" width="700px" destroy-on-close @close="stopStress">
      <el-form label-width="90px">
        <el-form-item label="并发数">
          <el-input-number v-model="stressForm.concurrency" :min="1" :max="1000" :step="1" :disabled="stressRunning" />
        </el-form-item>
        <el-form-item label="总请求数">
          <el-input-number v-model="stressForm.total" :min="1" :max="100000" :step="10" :disabled="stressRunning" />
        </el-form-item>
        <el-form-item label="超时(秒)">
          <el-input-number v-model="stressForm.timeout" :min="1" :max="120" :step="1" :disabled="stressRunning" />
        </el-form-item>
      </el-form>

      <div class="stress-progress" v-if="stressRunning">
        <el-progress
          :percentage="Math.round((stressProgress.current / stressProgress.total) * 100)"
          :text-inside="true"
          :stroke-width="20"
        />
        <p class="stress-progress-text">{{ stressProgress.current }} / {{ stressProgress.total }}</p>
      </div>

      <div class="stress-result" v-if="stressResult">
        <div class="stress-summary">
          <el-tag :type="stressResult.failure === 0 ? 'success' : 'warning'" size="large">
            {{ stressResult.failure === 0 ? '全部通过' : stressResult.success + ' 成功 / ' + stressResult.failure + ' 失败' }}
          </el-tag>
          <span class="stress-qps">QPS: {{ stressResult.qps }}</span>
          <span class="stress-dur">{{ stressResult.duration }} ms</span>
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

      <div class="stress-empty" v-if="!stressRunning && !stressResult">
        <p>使用当前请求参数发起并发压力测试</p>
        <p class="stress-empty-hint">URL: {{ buildURL() }}</p>
      </div>

      <template #footer>
        <el-button @click="showStressDialog = false" :disabled="stressRunning">关闭</el-button>
        <el-button type="warning" @click="runStress" :loading="stressRunning" :disabled="!buildURL()">
          {{ stressRunning ? '测试中...' : '开始压测' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 右键菜单 -->
    <ContextMenu v-model="ctxVisible" :x="ctxX" :y="ctxY" :items="ctxItems" @pick="onCtxPick" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onActivated, onDeactivated, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Events } from '@wailsio/runtime'
import { Delete, Select, Plus, Promotion, Setting, Connection, Folder, Download, CopyDocument, MoreFilled, TrendCharts, Search, InfoFilled, Document } from '@element-plus/icons-vue'
import CodeEditor from '../components/CodeEditor.vue'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import { EVENTS } from '../utils/wails'
import * as RestService from '../../bindings/spark/app/service/rest/restservice.js'
import type { RestRequest, RestResponse, StressTestRequest, StressTestResult } from '../../bindings/spark/app/service/types/models.js'

interface StressProgress {
  current: number
  total: number
}

interface KV {
  key: string
  value: string
  enabled?: boolean
}

interface TreeNode {
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

interface FolderEnvItem {
  id: number
  folderId: number
  name: string
  baseUrl: string
  isActive: boolean
  sort: number
}

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS', 'WS']

const current = ref({
  method: 'GET',
  url: '',
  headers: [] as KV[],
  params: [] as KV[],
  body: '',
})

const reqTab = ref('headers')
const resTab = ref('body')
const sending = ref(false)
const saving = ref(false)
const response = ref<RestResponse | null>(null)
const responseSeq = ref(0)
const bodyEditorRef = ref<InstanceType<typeof CodeEditor> | null>(null)
const respEditorRef = ref<InstanceType<typeof CodeEditor> | null>(null)

// 树
const treeRef = ref()
const treeData = ref<TreeNode[]>([])
const treeFilter = ref('')
const editingNode = ref<TreeNode | null>(null)
const editingRequestId = ref<number>(0)
const folderEffectiveBaseUrl = ref('')
const folderCommonHeaders = ref<KV[]>([])

// 右键菜单
const ctxVisible = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)
const ctxNode = ref<TreeNode | null>(null)
const ctxItems = ref<(CtxItem | 'divider')[]>([])

// 压力测试
const showStressDialog = ref(false)
const stressForm = ref({
  concurrency: 10,
  total: 100,
  timeout: 10,
})
const stressRunning = ref(false)
const stressProgress = ref<StressProgress>({ current: 0, total: 0 })
const stressResult = ref<StressTestResult | null>(null)
let stressTimer: ReturnType<typeof setInterval> | null = null

// 设置环境弹窗（目录右键：基础链接 + 公共请求头）
const showEnvDialog = ref(false)
const envTargetNode = ref<TreeNode | null>(null)
const folderEnvList = ref<FolderEnvItem[]>([])
const folderHeadersForm = ref<KV[]>([])
const savingEnv = ref(false)

// HTML 预览
const htmlPreview = ref(true)
const isHtmlResponse = computed(() => {
  if (!response.value) return false
  const ct = (response.value.headers || {})['Content-Type'] || (response.value.headers || {})['content-type'] || ''
  return ct.includes('html')
})

// Body 类型（JSON / Form-Data / Form-Urlencoded / Binary）
const bodyType = ref<'json' | 'form' | 'urlencoded' | 'binary'>('json')
const formFields = ref<KV[]>([])
const urlencodedFields = ref<KV[]>([])
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
const curlText = ref('')

// curl generate
type CodeLanguage = 'curl' | 'curl-single' | 'curl-cmd' | 'curl-ps' | 'python' | 'javascript' | 'go'
const codeLanguage = ref<CodeLanguage>('curl')
const genEditorFilename = computed(() => {
  const map: Record<string, string> = { python: 'request.py', javascript: 'request.js', go: 'request.go' }
  return map[codeLanguage.value] || 'curl.sh'
})
const genDialogTitle = computed(() => {
  const map: Record<string, string> = {
    curl: '生成 cURL 命令（Bash/Linux · 独立行）',
    'curl-single': '生成 cURL 命令（单行）',
    'curl-cmd': '生成 cURL 命令（CMD · 独立行）',
    'curl-ps': '生成 cURL 命令（PowerShell · 独立行）',
    python: '生成 Python 代码',
    javascript: '生成 JavaScript 代码',
    go: '生成 Go 代码',
  }
  return map[codeLanguage.value] || '生成代码'
})
const generatedCurl = ref('')
const showGenCurlDialog = ref(false)
const curlEditorRef = ref<InstanceType<typeof CodeEditor> | null>(null)

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
  await loadRoot()
})

onBeforeUnmount(() => {
  unWsMsg?.()
  unWsMsg = null
  if (wsConnId.value) {
    RestService.WSClose(wsConnId.value).catch(() => undefined)
  }
})

onDeactivated(() => {
  if (sseTimer) {
    clearInterval(sseTimer)
    sseTimer = null
  }
  unWsMsg?.()
  unWsMsg = null
  if (wsConnId.value) {
    RestService.WSClose(wsConnId.value).catch(() => {})
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
  return {
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
  const id = treeRef.value?.getCurrentKey?.()
  if (id == null) return null
  return findNodeById(id)
}

function findNodeById(id: number): TreeNode | null {
  const node = treeRef.value?.getNode(id)
  return node?.data ?? null
}

async function reloadParent(parentId: number) {
  try {
    const list = await RestService.ListChildren(parentId)
    const children = (list ?? []).map(toTreeNode)
    if (parentId === 0) {
      treeData.value = children
    } else {
      treeRef.value?.updateKeyChildren(parentId, children)
    }
  } catch (e: any) {
    ElMessage.error('刷新失败: ' + (e?.message || e))
  }
}

function onNodeClick(data: TreeNode) {
  if (data.type === 'request') {
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

function onBlankContext(event: MouseEvent) {
  event.preventDefault()
  ctxNode.value = null
  ctxItems.value = [
    { key: 'new-folder', label: '新建文件夹', icon: Folder },
    { key: 'new-request', label: '新建请求', icon: Plus },
  ]
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
  }
  items.push({ key: 'rename', label: '重命名', icon: 'Edit' })
  items.push({ key: 'delete', label: '删除', icon: Delete, danger: true })
  return items
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
      const parentId = target && target.type === 'folder' ? target.id : 0
      await createFolder(parentId)
      break
    }
    case 'new-request': {
      const parentId = target && target.type === 'folder' ? target.id : 0
      await createRequest(parentId)
      break
    }
    case 'set-folder-env':
      if (target) openEnvEditor(target)
      break
    case 'rename':
      if (target) await renameNode(target)
      break
    case 'delete':
      if (target) await deleteNode(target)
      break
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

async function renameNode(node: TreeNode) {
  try {
    const { value } = await ElMessageBox.prompt('请输入新名称', '重命名', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      inputValue: node.name,
    })
    if (!value || !value.trim() || value.trim() === node.name) return
    if (node.type === 'folder') {
      await RestService.RenameFolder(node.id, value.trim())
    } else {
      await RestService.RenameRequest(node.id, value.trim())
    }
    await reloadParent(node.parentId)
  } catch {
    // 取消
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
    await nextTick()
    bodyEditorRef.value?.setContent(item.body || '')
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
      targetNode = findNodeById(created.id)
      if (targetNode) {
        editingNode.value = targetNode
        editingRequestId.value = created.id
        await nextTick()
        treeRef.value?.setCurrentKey(created.id)
      }
    } catch (e: any) {
      ElMessage.error('创建请求失败: ' + (e?.message || e))
      return
    }
  }

  saving.value = true
  try {
    const body = bodyEditorRef.value?.getContent() ?? current.value.body
    current.value.body = body
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
    })
    ElMessage.success('保存成功')
  } catch (e: any) {
    ElMessage.error('保存失败: ' + (e?.message || e))
  } finally {
    saving.value = false
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
  bodyEditorRef.value?.setContent('')
}

// ========== 设置环境（目录右键：基础链接 + 公共请求头） ==========

async function openEnvEditor(node: TreeNode) {
  envTargetNode.value = node

  // 加载基础链接列表
  try {
    const list = await RestService.ListFolderEnvs(node.id)
    folderEnvList.value = (list ?? []) as FolderEnvItem[]
  } catch {
    folderEnvList.value = []
  }

  // 加载公共请求头
  try {
    if (node.commonHeaders) {
      folderHeadersForm.value = JSON.parse(node.commonHeaders).map((h: KV) => ({ enabled: true, ...h }))
    } else {
      folderHeadersForm.value = []
    }
  } catch {
    folderHeadersForm.value = []
  }

  showEnvDialog.value = true
}

function addEnvItem() {
  folderEnvList.value.push({
    id: 0,
    folderId: envTargetNode.value?.id ?? 0,
    name: '',
    baseUrl: '',
    isActive: folderEnvList.value.length === 0,
    sort: folderEnvList.value.length,
  })
}

function removeEnvItem(index: number) {
  folderEnvList.value.splice(index, 1)
}

async function setActiveEnvItem(item: FolderEnvItem, _index: number) {
  if (item.id) {
    try {
      await RestService.SetActiveFolderEnv(item.id)
      for (const e of folderEnvList.value) {
        e.isActive = e.id === item.id
      }
    } catch (e: any) {
      ElMessage.error('设置失败: ' + (e?.message || e))
    }
  } else {
    // 新条目，先标记
    for (const e of folderEnvList.value) {
      e.isActive = e === item
    }
  }
}

async function saveEnvSettings() {
  const target = envTargetNode.value
  if (!target) return

  savingEnv.value = true
  try {
    // 1. 保存所有基础链接
    for (const item of folderEnvList.value) {
      await RestService.SaveFolderEnv({
        id: item.id,
        folderId: target.id,
        name: item.name.trim(),
        baseUrl: item.baseUrl.trim(),
        isActive: item.isActive,
      })
    }
    // 删除已移除的基础链接（需要后台支持，这里先跳过）

    // 2. 保存公共请求头
    const validHeaders = folderHeadersForm.value.filter((h) => h.key.trim())
    await RestService.SetFolderCommonHeaders(target.id, JSON.stringify(validHeaders))

    showEnvDialog.value = false
    await reloadParent(target.parentId)

    // 刷新当前编辑请求的有效公共请求头
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

    ElMessage.success('环境设置已保存')
  } catch (e: any) {
    ElMessage.error('保存失败: ' + (e?.message || e))
  } finally {
    savingEnv.value = false
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

async function send() {
  const url = buildURL()
  if (!url) {
    ElMessage.warning('请输入 URL')
    return
  }

  const body = bodyEditorRef.value?.getContent() ?? current.value.body
  current.value.body = body

  const headers = buildHeaders()

  let reqBody = ''
  if (bodyType.value === 'json') {
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
    insecure: false,
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

  try {
    const resp = await RestService.Send(req)
    response.value = resp
    responseSeq.value++
    resTab.value = 'body'
    if (resp.error) {
      ElMessage.warning(resp.error)
    }
    if (resp.streaming && resp.streamId) {
      if (sseTimer) clearInterval(sseTimer)
      sseTimer = setInterval(async () => {
        try {
          const chunk = await RestService.ReadStreamChunks(resp.streamId!)
          if (chunk.data) {
            response.value!.body += chunk.data
            responseSeq.value++
          }
          if (chunk.done) {
            if (sseTimer) { clearInterval(sseTimer); sseTimer = null }
          }
        } catch {}
      }, 150)
    }
  } catch (e: any) {
    ElMessage.error('请求失败: ' + (e?.message || String(e)))
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
  curlText.value = ''
  showCurlDialog.value = true
}

function importCurl() {
  const cmd = curlText.value.trim()
  if (!cmd) {
    ElMessage.warning('请粘贴 curl 命令')
    return
  }
  try {
    const parsed = parseCurl(cmd)
    if (parsed.method) current.value.method = parsed.method
    if (parsed.url) current.value.url = parsed.url
    if (parsed.headers.length) {
      current.value.headers = parsed.headers.map((h: KV) => ({ ...h, enabled: true }))
    }
    if (parsed.body) {
      current.value.body = parsed.body
      bodyType.value = 'json'
    }
    if (parsed.params.length) {
      current.value.params = parsed.params.map((p: KV) => ({ ...p, enabled: true }))
    }
    showCurlDialog.value = false
    ElMessage.success('curl 导入成功')
  } catch (e: any) {
    ElMessage.error('解析失败: ' + (e?.message || e))
  }
}

function parseCurl(cmd: string): { method: string; url: string; headers: KV[]; body: string; params: KV[] } {
  const result: { method: string; url: string; headers: KV[]; body: string; params: KV[] } = {
    method: 'GET',
    url: '',
    headers: [],
    body: '',
    params: [],
  }

  let s = cmd.replace(/\\(\r?\n|$)/g, ' ')
  s = s.replace(/^curl\s+/i, '')

  const tokens = tokenize(s)

  let i = 0
  while (i < tokens.length) {
    const t = tokens[i]

    if (t === '-X' || t === '--request') {
      i++
      if (i < tokens.length) result.method = tokens[i].toUpperCase()
    } else if (t === '-H' || t === '--header') {
      i++
      if (i < tokens.length) {
        const idx = tokens[i].indexOf(':')
        if (idx > 0) {
          result.headers.push({
            key: tokens[i].slice(0, idx).trim(),
            value: tokens[i].slice(idx + 1).trim(),
          })
        }
      }
    } else if (t === '-d' || t === '--data' || t === '--data-raw' || t === '--data-binary') {
      i++
      if (i < tokens.length) {
        result.body = tokens[i]
        if (!result.method || result.method === 'GET') result.method = 'POST'
      }
    } else if (t === '-u' || t === '--user') {
      i++
      if (i < tokens.length) {
        result.headers.push({ key: 'Authorization', value: 'Basic ' + btoa(tokens[i]) })
      }
    } else if (t === '-G' || t === '--get') {
      result.method = 'GET'
    } else if (t === '-I' || t === '--head') {
      result.method = 'HEAD'
    } else if (t === '-b' || t === '--cookie') {
      i++
      if (i < tokens.length) {
        result.headers.push({ key: 'Cookie', value: tokens[i] })
      }
    } else if (t === '--compressed') {
      result.headers.push({ key: 'Accept-Encoding', value: 'gzip, deflate' })
    } else if (!t.startsWith('-') && !result.url) {
      result.url = t
    }

    i++
  }

  if (!result.url) {
    for (const tk of tokens) {
      if (/^https?:\/\//i.test(tk)) {
        result.url = tk
        break
      }
    }
  }

  if (result.url) {
    const qIdx = result.url.indexOf('?')
    if (qIdx >= 0) {
      const queryString = result.url.slice(qIdx + 1)
      result.url = result.url.slice(0, qIdx)
      if (queryString) {
        result.params = queryString.split('&').map((pair) => {
          const eqIdx = pair.indexOf('=')
          if (eqIdx >= 0) {
            return {
              key: decodeURIComponent(pair.slice(0, eqIdx)),
              value: decodeURIComponent(pair.slice(eqIdx + 1)),
            }
          }
          return { key: decodeURIComponent(pair), value: '' }
        })
      }
    }
  }

  return result
}

function tokenize(s: string): string[] {
  const tokens: string[] = []
  let i = 0
  while (i < s.length) {
    while (i < s.length && /\s/.test(s[i])) i++
    if (i >= s.length) break

    if (s[i] === "'") {
      i++
      let val = ''
      while (i < s.length && s[i] !== "'") {
        val += s[i]
        i++
      }
      i++
      tokens.push(val)
    } else if (s[i] === '"') {
      i++
      let val = ''
      while (i < s.length && s[i] !== '"') {
        if (s[i] === '\\' && i + 1 < s.length) {
          val += s[i + 1]
          i += 2
        } else {
          val += s[i]
          i++
        }
      }
      i++
      tokens.push(val)
    } else {
      let val = ''
      while (i < s.length && !/\s/.test(s[i])) {
        if (s[i] === '\\' && i + 1 < s.length) {
          val += s[i + 1]
          i += 2
        } else {
          val += s[i]
          i++
        }
      }
      tokens.push(val)
    }
  }
  return tokens
}

function shellQuote(s: string, shell: 'bash' | 'cmd' | 'powershell' = 'bash'): string {
  if (/^[a-zA-Z0-9_\-./:?=#%]+$/.test(s)) return s
  if (shell === 'cmd') {
    if (!s.includes('"')) return `"${s}"`
    return `"${s.replace(/"/g, '""')}"`
  }
  if (shell === 'powershell') {
    if (!s.includes("'")) return `'${s}'`
    if (!s.includes('"')) return `"${s}"`
    return `@"` + "\n" + `${s}` + "\n" + `"@`
  }
  if (!s.includes("'")) return `'${s}'`
  if (!s.includes('"')) return `"${s}"`
  return `'${s.replace(/'/g, "'\\''")}'`
}

function buildCurl(continuation: string, style: 'single' | 'multiline'): string {
  const url = buildURL()
  if (!url) return ''

  const shell: 'bash' | 'cmd' | 'powershell' =
    continuation === '^' ? 'cmd' : continuation === '`' ? 'powershell' : 'bash'
  const NL = continuation === '^' ? '\r\n' : '\n'

  const tokens: string[] = []
  tokens.push('curl')

  const method = current.value.method.toUpperCase()
  if (method !== 'GET') {
    tokens.push('-X', shellQuote(method, shell))
  }

  const headers = buildHeaders()
  for (const [k, v] of Object.entries(headers)) {
    if (k === 'User-Agent') continue
    tokens.push('-H', shellQuote(`${k}: ${v}`, shell))
  }

  if (current.value.body) {
    tokens.push('-d', shellQuote(current.value.body, shell))
  }

  tokens.push(shellQuote(url, shell))

  if (style === 'single') {
    return tokens.join(' ')
  }

  const lines: string[] = []
  for (let i = 0; i < tokens.length; i += 2) {
    const group = tokens.slice(i, i + 2).join(' ')
    lines.push(group)
  }
  if (lines.length === 0) return tokens.join(' ')
  return lines.map((l, idx) => (idx < lines.length - 1 ? l + ' ' + continuation : l)).join(NL)
}

function genCode() {
  const url = buildURL()
  if (!url) {
    ElMessage.warning('请输入 URL')
    return
  }
  generatedCurl.value = buildCode()
  showGenCurlDialog.value = true
  nextTick(() => {
    curlEditorRef.value?.setContent(generatedCurl.value)
  })
}

function buildCode(): string {
  const lang = codeLanguage.value
  if (lang === 'curl') return buildCurl('\\', 'multiline')
  if (lang === 'curl-single') return buildCurl('\\', 'single')
  if (lang === 'curl-cmd') return buildCurl('^', 'multiline')
  if (lang === 'curl-ps') return buildCurl('`', 'multiline')
  if (lang === 'python') return buildPython()
  if (lang === 'javascript') return buildJavaScript()
  if (lang === 'go') return buildGo()
  return ''
}

function jsQuote(s: string): string {
  return JSON.stringify(s)
}

function buildPython(): string {
  const url = buildURL()
  if (!url) return ''

  const method = current.value.method.toUpperCase()
  const headers = buildHeaders()
  const body = current.value.body || ''

  const lines: string[] = ['import requests', '']
  lines.push(`url = ${jsQuote(url)}`)
  if (Object.keys(headers).length > 0) {
    const headerLines = Object.entries(headers)
      .filter(([k]) => k !== 'User-Agent')
      .map(([k, v]) => `    ${jsQuote(k)}: ${jsQuote(v)}`)
    lines.push(`headers = {`)
    lines.push(headerLines.join(',\n'))
    lines.push(`}`)
  }
  if (body) {
    lines.push(`data = ${jsQuote(body)}`)
  }

  const args: string[] = ['url']
  if (Object.keys(headers).filter(k => k !== 'User-Agent').length > 0) args.push('headers=headers')
  if (body) args.push('data=data')

  const methodLower = method.toLowerCase()
  lines.push(`response = requests.${methodLower}(${args.join(', ')})`)
  lines.push('print(response.text)')

  return lines.join('\n')
}

function buildJavaScript(): string {
  const url = buildURL()
  if (!url) return ''

  const method = current.value.method.toUpperCase()
  const headers = buildHeaders()
  const body = current.value.body || ''

  const filteredHeaders = Object.entries(headers).filter(([k]) => k !== 'User-Agent')
  const hasHeaders = filteredHeaders.length > 0

  const lines: string[] = []
  lines.push(`const url = ${jsQuote(url)};`)
  lines.push('')
  lines.push(`const options = {`)
  lines.push(`  method: ${jsQuote(method)},`)
  if (hasHeaders) {
    lines.push('  headers: {')
    filteredHeaders.forEach(([k, v], i) => {
      lines.push(`    ${jsQuote(k)}: ${jsQuote(v)}` + (i < filteredHeaders.length - 1 ? ',' : ''))
    })
    lines.push('  },')
  }
  if (body) {
    lines.push(`  body: ${jsQuote(body)},`)
  }
  lines.push('};')
  lines.push('')
  lines.push('fetch(url, options)')
  lines.push('  .then(res => res.json())')
  lines.push('  .then(data => console.log(data));')

  return lines.join('\n')
}

function buildGo(): string {
  const url = buildURL()
  if (!url) return ''

  const method = current.value.method.toUpperCase()
  const headers = buildHeaders()
  const body = current.value.body || ''
  const hasBody = body.length > 0

  const lines: string[] = [
    'package main',
    '',
    'import (',
  ]
  if (hasBody) {
    lines.push('    "fmt"', '    "io"', '    "net/http"', '    "strings"')
  } else {
    lines.push('    "fmt"', '    "io"', '    "net/http"')
  }
  lines.push(')', '')
  lines.push('func main() {')
  lines.push(`    url := ${jsQuote(url)}`)
  lines.push(`    method := ${jsQuote(method)}`)
  lines.push('')

  if (hasBody) {
    lines.push(`    payload := strings.NewReader(${jsQuote(body)})`)
    lines.push(`    req, err := http.NewRequest(method, url, payload)`)
  } else {
    lines.push(`    req, err := http.NewRequest(method, url, nil)`)
  }
  lines.push('    if err != nil {')
  lines.push('        fmt.Println(err)')
  lines.push('        return')
  lines.push('    }')
  lines.push('')

  for (const [k, v] of Object.entries(headers)) {
    if (k === 'User-Agent') continue
    lines.push(`    req.Header.Add(${jsQuote(k)}, ${jsQuote(v)})`)
  }
  lines.push('')
  lines.push('    res, err := http.DefaultClient.Do(req)')
  lines.push('    if err != nil {')
  lines.push('        fmt.Println(err)')
  lines.push('        return')
  lines.push('    }')
  lines.push('    defer res.Body.Close()')
  lines.push('')
  lines.push('    body, err := io.ReadAll(res.Body)')
  lines.push('    if err != nil {')
  lines.push('        fmt.Println(err)')
  lines.push('        return')
  lines.push('    }')
  lines.push('    fmt.Println(string(body))')
  lines.push('}')

  return lines.join('\n')
}

watch(codeLanguage, () => {
  const code = buildCode()
  generatedCurl.value = code
  curlEditorRef.value?.setContent(code)
})

function copyGeneratedCurl() {
  const text = curlEditorRef.value?.getContent() ?? generatedCurl.value
  navigator.clipboard.writeText(text).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    ElMessage.warning('复制失败，请手动选择复制')
  })
}

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
    try { await RestService.WSClose(wsConnId.value) } catch {}
  }
  wsConnected.value = false
  wsConnId.value = ''
}

// ========== 压力测试 ==========

async function runStress() {
  const url = buildURL()
  if (!url) {
    ElMessage.warning('请输入 URL')
    return
  }

  const body = bodyEditorRef.value?.getContent() ?? current.value.body
  current.value.body = body

  stressResult.value = null
  stressRunning.value = true
  stressProgress.value = { current: 0, total: stressForm.value.total }

  stressTimer = setInterval(() => {
    if (stressProgress.value.current < stressForm.value.total) {
      stressProgress.value.current = Math.min(
        stressProgress.value.current + stressForm.value.concurrency * 2,
        stressForm.value.total,
      )
    }
  }, 300)

  try {
    const req: StressTestRequest = {
      method: current.value.method,
      url,
      headers: buildHeaders(),
      body: current.value.body || '',
      timeout: stressForm.value.timeout,
      concurrency: stressForm.value.concurrency,
      total: stressForm.value.total,
      insecure: false,
    }
    const result = await RestService.StressTest(req)
    stressResult.value = result
    stressProgress.value.current = stressForm.value.total
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

.tree-method.method-get { color: var(--success-color); }
.tree-method.method-post { color: var(--warning-color); }
.tree-method.method-put { color: #e6a23c; }
.tree-method.method-delete { color: var(--danger-color); }
.tree-method.method-patch { color: #e6a23c; }
.tree-method.method-head { color: var(--text-secondary); }
.tree-method.method-options { color: var(--text-secondary); }

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

.method-select.method-get :deep(.el-input__wrapper) { color: var(--success-color); }
.method-select.method-post :deep(.el-input__wrapper) { color: var(--warning-color); }
.method-select.method-put :deep(.el-input__wrapper) { color: #e6a23c; }
.method-select.method-delete :deep(.el-input__wrapper) { color: var(--danger-color); }
.method-select.method-patch :deep(.el-input__wrapper) { color: #e6a23c; }
.method-select.method-head :deep(.el-input__wrapper) { color: var(--text-secondary); }
.method-select.method-options :deep(.el-input__wrapper) { color: var(--text-secondary); }

.url-input { flex: 1; }

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

.kv-key { width: 200px; flex-shrink: 0; }
.kv-val { flex: 1; }
.kv-del { flex-shrink: 0; }

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

.html-toggle { margin-left: auto; }

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

/* 设置环境弹窗 */
.env-tab-hint {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--text-tertiary);
  margin-bottom: 12px;
  padding: 6px 10px;
  background: var(--bg-tertiary);
  border-radius: 4px;
}

.env-base-url-list {
  max-height: 450px;
  overflow-y: auto;
}

.env-base-item {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
  padding: 6px 8px;
  background: var(--bg-secondary);
  border-radius: 6px;
}

.env-base-name {
  width: 120px;
  flex-shrink: 0;
}

.env-base-url {
  flex: 1;
}

.env-active-tag {
  flex-shrink: 0;
}

/* 压力测试 */
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

/* curl */
.curl-gen-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.curl-gen-label {
  font-size: 13px;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.curl-gen-copy {
  margin-left: auto;
}

.curl-gen-editor {
  height: 380px;
  border-radius: 6px;
  overflow: hidden;
  border: 1px solid var(--border-color);
}

.toolbar-more {
  margin-left: auto;
}
</style>