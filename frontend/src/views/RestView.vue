<template>
  <div class="rest-view">
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
        placeholder="输入请求 URL，例如 https://api.example.com/users"
        clearable
        @keydown.enter="send"
      />
      <el-button type="primary" :loading="sending" @click="send">
        <el-icon v-if="!sending"><Promotion /></el-icon>
        <span>发送</span>
      </el-button>
    </div>

    <div class="rest-body">
      <div class="rest-request">
        <el-tabs v-model="reqTab" class="rest-tabs">
          <el-tab-pane label="Headers" name="headers">
            <div class="kv-editor">
              <div class="kv-row" v-for="(row, i) in current.headers" :key="i">
                <el-input
                  v-model="row.key"
                  class="kv-key"
                  placeholder="Header 名"
                  @input="onHeaderChange(i)"
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
              <el-button size="small" :icon="Plus" @click="current.headers.push({ key: '', value: '' })">
                添加 Header
              </el-button>
            </div>
          </el-tab-pane>
          <el-tab-pane label="Query" name="query">
            <div class="kv-editor">
              <div class="kv-row" v-for="(row, i) in current.params" :key="i">
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
              <el-button size="small" :icon="Plus" @click="current.params.push({ key: '', value: '' })">
                添加参数
              </el-button>
            </div>
          </el-tab-pane>
          <el-tab-pane label="Body" name="body">
            <div class="code-wrap">
              <CodeEditor
                ref="bodyEditorRef"
                filename="request.json"
                wrap
              />
            </div>
          </el-tab-pane>
        </el-tabs>
      </div>

      <div class="rest-response" v-if="response">
        <div class="response-status-bar">
          <el-tag
            :type="statusTagType"
            size="large"
            effect="dark"
          >
            {{ response.status }} {{ response.statusText }}
          </el-tag>
          <span class="response-duration">{{ response.duration }} ms</span>
          <span class="response-size">{{ formatSize(response.body?.length || 0) }}</span>
        </div>
        <el-tabs v-model="resTab" class="rest-tabs">
          <el-tab-pane label="Body" name="body">
            <div class="code-wrap">
              <CodeEditor
                ref="respEditorRef"
                :key="'resp-' + responseSeq"
                :filename="respFilename"
                wrap
              />
            </div>
          </el-tab-pane>
          <el-tab-pane label="Headers" name="headers">
            <div class="kv-editor readonly">
              <div class="kv-row" v-for="(val, key) in (response.headers || {})" :key="key">
                <span class="kv-key-ro">{{ key }}</span>
                <span class="kv-val-ro">{{ val }}</span>
              </div>
              <p v-if="!Object.keys(response.headers || {}).length" class="kv-empty">无响应头</p>
            </div>
          </el-tab-pane>
        </el-tabs>
      </div>

      <div class="rest-response empty" v-else-if="!sending">
        <div class="response-empty-hint">
          <el-icon :size="36" color="var(--border-strong)"><Promotion /></el-icon>
          <p>点击发送按钮发起请求</p>
        </div>
      </div>
    </div>

    <div class="rest-sidebar" v-if="history.length > 0">
      <div class="history-head">
        <span class="history-title">历史</span>
        <el-button size="small" text @click="clearHistory">清空</el-button>
      </div>
      <div class="history-list">
        <div
          v-for="(h, i) in history"
          :key="i"
          class="history-item"
          @click="loadHistory(h)"
        >
          <span class="history-method" :class="'method-' + h.method.toLowerCase()">{{ h.method }}</span>
          <span class="history-url" :title="h.url">{{ h.url }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Delete, Plus, Promotion } from '@element-plus/icons-vue'
import CodeEditor from '../components/CodeEditor.vue'
import { RestService, type RestRequest, type RestResponse } from '../utils/wails'

interface KV {
  key: string
  value: string
}

interface HistoryEntry {
  method: string
  url: string
  headers: KV[]
  params: KV[]
  body: string
}

const methods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS']

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
const response = ref<RestResponse | null>(null)
const responseSeq = ref(0)
const bodyEditorRef = ref<InstanceType<typeof CodeEditor> | null>(null)
const respEditorRef = ref<InstanceType<typeof CodeEditor> | null>(null)

const history = ref<HistoryEntry[]>(loadHistoryFromStorage())

watch(responseSeq, async () => {
  await nextTick()
  if (response.value?.body != null) {
    respEditorRef.value?.setContent(response.value.body)
  }
})

const methodClass = computed(() => 'method-' + current.value.method.toLowerCase())

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
  const ct = (response.value.headers || {})['Content-Type'] || (response.value.headers || {})['content-type'] || (response.value.headers || {})['Content-type'] || ''
  if (ct.includes('json')) return 'response.json'
  if (ct.includes('xml') || ct.includes('html')) return ct.includes('html') ? 'response.html' : 'response.xml'
  if (ct.includes('javascript')) return 'response.js'
  if (ct.includes('css')) return 'response.css'
  return 'response.txt'
})

function saveHistory() {
  const entry: HistoryEntry = {
    method: current.value.method,
    url: current.value.url,
    headers: JSON.parse(JSON.stringify(current.value.headers.filter((h) => h.key))),
    params: JSON.parse(JSON.stringify(current.value.params.filter((p) => p.key))),
    body: current.value.body,
  }
  const idx = history.value.findIndex((h) => h.method === entry.method && h.url === entry.url)
  if (idx >= 0) history.value.splice(idx, 1)
  history.value.unshift(entry)
  if (history.value.length > 50) history.value.splice(50)
  persistHistory()
}

function persistHistory() {
  try {
    localStorage.setItem('rest-history', JSON.stringify(history.value))
  } catch { /* ignore */ }
}

function loadHistoryFromStorage(): HistoryEntry[] {
  try {
    const raw = localStorage.getItem('rest-history')
    return raw ? JSON.parse(raw) : []
  } catch {
    return []
  }
}

function loadHistory(h: HistoryEntry) {
  current.value.method = h.method
  current.value.url = h.url
  current.value.headers = h.headers.length ? JSON.parse(JSON.stringify(h.headers)) : []
  current.value.params = h.params.length ? JSON.parse(JSON.stringify(h.params)) : []
  current.value.body = h.body
  nextTick(() => {
    bodyEditorRef.value?.setContent(h.body)
  })
}

function clearHistory() {
  history.value = []
  try { localStorage.removeItem('rest-history') } catch { /* ignore */ }
}

function buildURL(): string {
  let url = current.value.url.trim()
  if (!url) return ''
  if (!/^https?:\/\//i.test(url)) {
    url = 'https://' + url
  }
  const params = current.value.params.filter((p) => p.key)
  if (params.length > 0) {
    const sep = url.includes('?') ? '&' : '?'
    url += sep + params.map((p) => encodeURIComponent(p.key) + '=' + encodeURIComponent(p.value)).join('&')
  }
  return url
}

function buildHeaders(): Record<string, string> {
  const h: Record<string, string> = {}
  for (const row of current.value.headers) {
    if (row.key.trim()) {
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

  const req: RestRequest = {
    method: current.value.method,
    url,
    headers: buildHeaders(),
    body: current.value.body || '',
    timeout: 30,
  }

  sending.value = true
  response.value = null

  try {
    const resp = await RestService.Send(req)
    response.value = resp
    responseSeq.value++
    resTab.value = 'body'
    saveHistory()
    if (resp.error) {
      ElMessage.warning(resp.error)
    }
  } catch (e: any) {
    ElMessage.error('请求失败: ' + (e?.message || String(e)))
  } finally {
    sending.value = false
  }
}

function onHeaderChange(i: number) {
  // header 编辑不需要额外处理
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}
</script>

<style scoped>
.rest-view {
  display: flex;
  flex-direction: column;
  height: 100vh;
  overflow: hidden;
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

.rest-body {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
}

.rest-request {
  flex: 0 0 50%;
  display: flex;
  flex-direction: column;
  border-bottom: 2px solid var(--border-color);
  overflow: hidden;
  min-height: 0;
}

.rest-response {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
}

.rest-response.empty {
  display: flex;
  align-items: center;
  justify-content: center;
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
  font-family: var(--font-mono, 'Cascadia Code', 'Fira Code', monospace);
  font-size: 13px;
  color: var(--text-primary);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kv-val-ro {
  flex: 1;
  font-family: var(--font-mono, 'Cascadia Code', 'Fira Code', monospace);
  font-size: 13px;
  color: var(--text-secondary);
  word-break: break-all;
}

.kv-empty {
  color: var(--text-secondary);
  font-size: 13px;
  margin-top: 20px;
  text-align: center;
}

.code-wrap {
  height: 100%;
  overflow: hidden;
}

.rest-sidebar {
  display: none;
  position: absolute;
  right: 0;
  top: 0;
  bottom: 0;
  width: 260px;
  background: var(--bg-primary);
  border-left: 1px solid var(--border-color);
  flex-direction: column;
  overflow: hidden;
  z-index: 10;
}

.history-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color);
}

.history-title {
  font-weight: 600;
  font-size: 13px;
  color: var(--text-secondary);
}

.history-list {
  flex: 1;
  overflow-y: auto;
}

.history-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  cursor: pointer;
  font-size: 12px;
  border-bottom: 1px solid var(--border-light, transparent);
  transition: background 0.15s;
}

.history-item:hover {
  background: var(--bg-hover);
}

.history-method {
  flex-shrink: 0;
  font-weight: 700;
  font-size: 11px;
  min-width: 38px;
}

.history-method.method-get { color: var(--success-color); }
.history-method.method-post { color: var(--warning-color); }
.history-method.method-put { color: #e6a23c; }
.history-method.method-delete { color: var(--danger-color); }
.history-method.method-patch { color: #e6a23c; }
.history-method.method-head { color: var(--text-secondary); }
.history-method.method-options { color: var(--text-secondary); }

.history-url {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-secondary);
}
</style>