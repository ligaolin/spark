<template>
  <div class="rest-view">
    <el-splitter :layout="'horizontal'" class="root-splitter">
      <el-splitter-panel :size="sidebarSize" :min="180" :max="520" @resize="onSidebarResize">
        <!-- 左侧边栏：目录树 + 环境 -->
        <div class="rest-sidebar">
          <!-- 环境选择器 -->
          <div class="sidebar-env">
            <el-select
              v-model="activeEnvId"
              class="env-select"
              placeholder="选择环境"
              clearable
              @change="onEnvChange"
            >
              <el-option
                v-for="env in environments"
                :key="env.id"
                :label="env.name + (env.isDefault ? ' (默认)' : '')"
                :value="env.id"
              />
            </el-select>
            <el-button size="small" text :icon="Setting" @click="showEnvDialog = true" title="管理环境" />
          </div>

          <!-- 树搜索 -->
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

          <!-- 树 -->
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
          <el-icon><Plus /></el-icon>
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
              <el-dropdown-item @click="genCurl">
                <el-icon><CopyDocument /></el-icon>生成 curl
              </el-dropdown-item>
              <el-dropdown-item divided @click="showStressDialog = true">
                <el-icon><TrendCharts /></el-icon>压力测试
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>

      <!-- 环境提示 -->
      <div class="env-hint" v-if="activeEnv && current.url && !isAbsoluteURL(current.url)">
        <el-icon><Connection /></el-icon>
        <span>{{ activeEnv.baseUrl }}{{ current.url }}</span>
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

    <!-- 环境管理弹窗 -->
    <el-dialog v-model="showEnvDialog" title="环境管理" width="650px" destroy-on-close>
      <div class="env-list">
        <div class="env-item" v-for="env in environments" :key="env.id">
          <div class="env-item-row">
            <span class="env-name">{{ env.name }}</span>
            <span class="env-url" :title="env.baseUrl">{{ env.baseUrl || '(无基础链接)' }}</span>
            <el-tag v-if="env.isDefault" size="small" type="success">默认</el-tag>
            <div class="env-actions">
              <el-button size="small" text @click="editEnv(env)">编辑</el-button>
              <el-button v-if="!env.isDefault" size="small" text @click="setDefaultEnv(env.id)">设为默认</el-button>
              <el-button size="small" text type="danger" @click="deleteEnv(env.id)">删除</el-button>
            </div>
          </div>
          <div class="env-headers" v-if="env.commonHeaders && env.commonHeaders.length">
            <span class="env-headers-label">公共请求头：</span>
            <el-tag v-for="h in env.commonHeaders" :key="h.key" size="small" class="env-header-tag">
              {{ h.key }}: {{ h.value }}
            </el-tag>
          </div>
        </div>
        <p v-if="!environments.length" class="env-empty">暂无环境，点击下方按钮新建</p>
      </div>
      <template #footer>
        <el-button @click="editEnv(null)">新建环境</el-button>
        <el-button @click="showEnvDialog = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 环境编辑弹窗 -->
    <el-dialog v-model="envEditVisible" :title="editingEnvId ? '编辑环境' : '新建环境'" width="600px" destroy-on-close>
      <el-form label-width="90px" v-if="envEditVisible">
        <el-form-item label="名称">
          <el-input v-model="envForm.name" placeholder="例如：生产环境" />
        </el-form-item>
        <el-form-item label="基础链接">
          <el-input v-model="envForm.baseUrl" placeholder="例如：https://api.example.com" />
        </el-form-item>
        <el-form-item label="默认环境">
          <el-switch v-model="envForm.isDefault" />
        </el-form-item>
        <el-form-item label="公共请求头">
          <div class="kv-editor">
            <div class="kv-row" v-for="(row, i) in envForm.commonHeaders" :key="i">
              <el-input v-model="row.key" class="kv-key" placeholder="Header 名" />
              <el-input v-model="row.value" class="kv-val" placeholder="Header 值" />
              <el-button size="small" :icon="Delete" circle @click="envForm.commonHeaders.splice(i, 1)" />
            </div>
            <el-button size="small" :icon="Plus" @click="envForm.commonHeaders.push({ key: '', value: '' })">
              添加公共请求头
            </el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="envEditVisible = false">取消</el-button>
        <el-button type="primary" @click="saveEnv">保存</el-button>
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
    <el-dialog v-model="showGenCurlDialog" title="生成 curl 命令" width="750px" destroy-on-close>
      <div class="curl-gen-toolbar">
        <span class="curl-gen-label">换行风格：</span>
        <el-radio-group v-model="curlLineStyle" size="small">
          <el-radio-button value="backslash">反斜杠续行</el-radio-button>
          <el-radio-button value="cmd">CMD 续行</el-radio-button>
          <el-radio-button value="multiline">独立行</el-radio-button>
          <el-radio-button value="single">单行</el-radio-button>
        </el-radio-group>
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
          filename="curl.sh"
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
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Events } from '@wailsio/runtime'
import { Delete, Plus, Promotion, Setting, Connection, Folder, Download, CopyDocument, MoreFilled, TrendCharts, Search } from '@element-plus/icons-vue'
import CodeEditor from '../components/CodeEditor.vue'
import ContextMenu from '../components/ContextMenu.vue'
import type { CtxItem } from '../components/ContextMenu.vue'
import { EVENTS } from '../utils/wails'
import * as RestService from '../../bindings/changeme/app/service/rest/restservice.js'
import type { RestRequest, RestResponse, StressTestRequest, StressTestResult } from '../../bindings/changeme/app/service/types/models'

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
  leaf: boolean
  sort: number
  children?: TreeNode[]
}

interface EnvItem {
  id: number
  name: string
  baseUrl: string
  commonHeaders: KV[]
  isDefault: boolean
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
const editingRequestId = ref<number>(0) // ID of the RestRequestModel being edited

// 环境
const environments = ref<EnvItem[]>([])
const activeEnvId = ref<number | null>(null)
const showEnvDialog = ref(false)
const envEditVisible = ref(false)
const editingEnvId = ref<number>(0)
const envForm = ref({
  name: '',
  baseUrl: '',
  isDefault: false,
  commonHeaders: [] as KV[],
})

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

// Splitter 尺寸（el-splitter 的 size 支持像素或百分比，用字符串）
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
type CurlLineStyle = 'single' | 'backslash' | 'multiline' | 'cmd'
const curlLineStyle = ref<CurlLineStyle>('backslash')
const generatedCurl = ref('')
const showGenCurlDialog = ref(false)
const curlEditorRef = ref<InstanceType<typeof CodeEditor> | null>(null)

// 计算
const methodClass = computed(() => 'method-' + current.value.method.toLowerCase())

const activeEnv = computed(() => {
  if (!activeEnvId.value) return null
  return environments.value.find((e) => e.id === activeEnvId.value) ?? null
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
  await loadEnvironments()
  await loadRoot()
})

onBeforeUnmount(() => {
  unWsMsg?.()
  unWsMsg = null
  if (wsConnId.value) {
    RestService.WSClose(wsConnId.value).catch(() => undefined)
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
    current.value.headers = item.headers
      ? JSON.parse(JSON.stringify(item.headers)).map((h: KV) => ({ enabled: true, ...h }))
      : []
    current.value.params = item.params
      ? JSON.parse(JSON.stringify(item.params)).map((p: KV) => ({ enabled: true, ...p }))
      : []
    current.value.body = item.body || ''
    await nextTick()
    bodyEditorRef.value?.setContent(item.body || '')
    // 如果选中了环境且 URL 不是绝对地址，显示基础链接提示
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
  current.value.method = 'GET'
  current.value.url = ''
  current.value.headers = []
  current.value.params = []
  current.value.body = ''
  bodyEditorRef.value?.setContent('')
}

// ========== 环境 ==========

async function loadEnvironments() {
  try {
    const list = await RestService.ListEnvironments()
    environments.value = (list ?? []) as EnvItem[]
    // 默认环境自动选中
    const def = environments.value.find((e) => e.isDefault)
    if (def) {
      activeEnvId.value = def.id
    }
  } catch (e: any) {
    // ignore
  }
}

function onEnvChange() {
  // 切换环境时无需额外操作，发送时自动拼接基础链接和公共请求头
}

function editEnv(env: EnvItem | null) {
  if (env) {
    editingEnvId.value = env.id
    envForm.value = {
      name: env.name,
      baseUrl: env.baseUrl,
      isDefault: env.isDefault,
      commonHeaders: env.commonHeaders
        ? JSON.parse(JSON.stringify(env.commonHeaders)).map((h) => ({ enabled: true, ...h }))
        : [],
    }
  } else {
    editingEnvId.value = 0
    envForm.value = {
      name: '',
      baseUrl: '',
      isDefault: environments.value.length === 0,
      commonHeaders: [],
    }
  }
  envEditVisible.value = true
}

async function saveEnv() {
  if (!envForm.value.name.trim()) {
    ElMessage.warning('请输入环境名称')
    return
  }
  try {
    await RestService.SaveEnvironment({
      id: editingEnvId.value,
      name: envForm.value.name.trim(),
      baseUrl: envForm.value.baseUrl.trim(),
      commonHeaders: envForm.value.commonHeaders.filter((h) => h.key.trim()),
      isDefault: envForm.value.isDefault,
    })
    envEditVisible.value = false
    await loadEnvironments()
    ElMessage.success(editingEnvId.value ? '环境已更新' : '环境已创建')
  } catch (e: any) {
    ElMessage.error('保存环境失败: ' + (e?.message || e))
  }
}

async function setDefaultEnv(id: number) {
  try {
    await RestService.SetDefaultEnvironment(id)
    await loadEnvironments()
    ElMessage.success('已设为默认环境')
  } catch (e: any) {
    ElMessage.error('设置失败: ' + (e?.message || e))
  }
}

async function deleteEnv(id: number) {
  try {
    await ElMessageBox.confirm('确定删除该环境？', '删除', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning',
    })
    await RestService.DeleteEnvironment(id)
    await loadEnvironments()
    ElMessage.success('环境已删除')
  } catch {
    // 取消
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

  if (!isAbsoluteURL(url) && activeEnv.value && activeEnv.value.baseUrl) {
    const base = activeEnv.value.baseUrl.replace(/\/+$/, '')
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

  if (activeEnv.value && activeEnv.value.commonHeaders) {
    for (const row of activeEnv.value.commonHeaders) {
      if (row.key.trim() && row.enabled !== false) {
        h[row.key.trim()] = row.value
      }
    }
  }

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
    // SSE 流式处理
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

  // 预处理：去掉换行符的反斜杠续行，合并为一行
  let s = cmd.replace(/\\(\r?\n|$)/g, ' ')

  // 去掉开头的 "curl "
  s = s.replace(/^curl\s+/i, '')

  // 提取 URL（第一个非选项参数）
  // 先提取所有 token
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
      // 第一个非选项参数当作 URL
      result.url = t
    }

    i++
  }

  // 如果没找到 URL，尝试从 tokens 中匹配 http(s) 开头的内容
  if (!result.url) {
    for (const tk of tokens) {
      if (/^https?:\/\//i.test(tk)) {
        result.url = tk
        break
      }
    }
  }

  return result
}

// 将 shell 命令 tokenize，处理单引号和双引号
function tokenize(s: string): string[] {
  const tokens: string[] = []
  let i = 0
  while (i < s.length) {
    // 跳过空白
    while (i < s.length && /\s/.test(s[i])) i++
    if (i >= s.length) break

    if (s[i] === "'") {
      i++
      let val = ''
      while (i < s.length && s[i] !== "'") {
        val += s[i]
        i++
      }
      i++ // 跳过结束的单引号
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
      i++ // 跳过结束的双引号
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

function shellQuote(s: string, shell: 'bash' | 'cmd' = 'bash'): string {
  if (/^[a-zA-Z0-9_\-./:?=#%]+$/.test(s)) return s
  if (shell === 'cmd') {
    if (!s.includes('"')) return `"${s}"`
    return `"${s.replace(/"/g, '""')}"`
  }
  if (!s.includes("'")) return `'${s}'`
  if (!s.includes('"')) return `"${s}"`
  return `'${s.replace(/'/g, "'\\''")}'`
}

function buildCurl(style: CurlLineStyle): string {
  const url = buildURL()
  if (!url) return ''

  const shell: 'bash' | 'cmd' = style === 'cmd' ? 'cmd' : 'bash'
  const NL = style === 'cmd' ? '\r\n' : '\n'

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

  if (style === 'cmd') {
    const lines: string[] = []
    for (let i = 0; i < tokens.length; i += 2) {
      const group = tokens.slice(i, i + 2).join(' ')
      lines.push(group)
    }
    if (lines.length === 0) return tokens.join(' ')
    return lines.map((l, idx) => (idx < lines.length - 1 ? l + '^' : l)).join(NL)
  }

  if (style === 'backslash') {
    const lines: string[] = []
    for (let i = 0; i < tokens.length; i += 2) {
      const group = tokens.slice(i, i + 2).join(' ')
      lines.push(group)
    }
    if (lines.length === 0) return tokens.join(' ')
    return lines.map((l, idx) => (idx < lines.length - 1 ? l + ' \\' : l)).join(NL)
  }

  if (style === 'multiline') {
    if (tokens.length <= 2) return tokens.join(' ')
    const [first, ...rest] = tokens
    return first + NL + '  ' + rest.join(NL + '  ')
  }

  return tokens.join(' ')
}

function genCurl() {
  const url = buildURL()
  if (!url) {
    ElMessage.warning('请输入 URL')
    return
  }
  generatedCurl.value = buildCurl(curlLineStyle.value)
  showGenCurlDialog.value = true
  nextTick(() => {
    curlEditorRef.value?.setContent(generatedCurl.value)
  })
}

watch(curlLineStyle, (style) => {
  const cmd = buildCurl(style)
  generatedCurl.value = cmd
  curlEditorRef.value?.setContent(cmd)
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

  // Simulate progress with a timer since Go call is blocking
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

.root-splitter :deep(.el-splitter-panel > div),
.body-splitter :deep(.el-splitter-panel > div) {
  height: 100%;
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

/* 左侧边栏 */
.rest-sidebar {
  display: flex;
  flex-direction: column;
  background: var(--bg-secondary);
  overflow: hidden;
}

.sidebar-env {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}

.env-select {
  flex: 1;
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

/* 主区域 */
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

/* HTML 预览 */
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

/* Body 类型切换 */
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

/* 环境管理 */
.env-list {
  max-height: 400px;
  overflow-y: auto;
}

.env-item {
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-color);
}

.env-item:last-child {
  border-bottom: none;
}

.env-item-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.env-name {
  font-weight: 600;
  flex-shrink: 0;
}

.env-url {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: var(--text-secondary);
}

.env-actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.env-headers {
  margin-top: 6px;
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

.env-headers-label {
  font-size: 12px;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.env-header-tag {
  font-size: 11px;
}

.env-empty {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
  font-size: 14px;
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