<template>
  <el-dialog
    v-model="visible"
    class="text-editor-dialog"
    width="88%"
    top="5vh"
    :close-on-click-modal="false"
    :before-close="beforeClose"
    @opened="onOpened"
    @closed="onClosed"
  >
    <template #header>
      <div class="editor-header">
        <el-icon><EditPen /></el-icon>
        <span class="editor-title">{{ fileName || '编辑器' }}</span>
        <span v-if="dirty" class="dirty-tag">● 未保存</span>
      </div>
    </template>

    <div class="editor-toolbar">
      <span class="editor-path" :title="fullPath">{{ fullPath }}</span>
      <div class="toolbar-actions">
        <el-button size="small" type="primary" :loading="saving" :disabled="!ready" @click="save">
          保存
        </el-button>
      </div>
    </div>

    <div class="editor-wrap">
      <CodeEditor ref="editorRef" :filename="fileName" :wrap="wrap" @change="onChange" @save="save" />
      <div v-if="loading" class="editor-loading-overlay">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>正在读取文件…</span>
      </div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { EditPen, Loading } from '@element-plus/icons-vue'
import CodeEditor from './CodeEditor.vue'
import { showConfirmDialog } from '../utils/dialog'
import { useSettingsStore } from '../stores/settings'
import type { FileBackend } from '../utils/fileBackend'

const settings = useSettingsStore()
const wrap = ref(true)

const visible = ref(false)
const fileName = ref('')
const fullPath = ref('')
const loading = ref(false)
const ready = ref(false)
const saving = ref(false)
const dirty = ref(false)

const editorRef = ref<InstanceType<typeof CodeEditor>>()

let currentBackend: FileBackend | null = null
let currentFile: { path: string; name: string } | null = null
let currentLineNo: number | undefined
let originalContent = ''
let initToken = 0

async function open(
  backend: FileBackend,
  file: { path: string; name: string },
  lineNo?: number,
) {
  currentBackend = backend
  currentFile = file
  currentLineNo = lineNo
  fileName.value = file.name
  fullPath.value = file.path
  originalContent = ''
  ready.value = false
  dirty.value = false
  wrap.value = settings.editorWordWrap
  if (visible.value) {
    await init()
  } else {
    visible.value = true
  }
}

function onOpened() {
  void init()
}

async function init() {
  if (!currentBackend || !currentFile) return
  const token = ++initToken
  loading.value = true
  try {
    const content = await currentBackend.readFile(currentFile.path)
    if (token !== initToken) return
    originalContent = content
    editorRef.value?.setContent(content)
    if (currentLineNo) editorRef.value?.jumpToLine(currentLineNo)
    else editorRef.value?.focus()
    ready.value = true
  } catch (e: any) {
    if (token !== initToken) return
    ElMessage.error(`打开文件失败：${e?.message || e}`)
    visible.value = false
  } finally {
    if (token === initToken) loading.value = false
  }
}

function onChange(value: string) {
  dirty.value = value !== originalContent
}

async function save() {
  if (!currentBackend || !currentFile || !ready.value) return
  const content = editorRef.value?.getContent() ?? ''
  saving.value = true
  try {
    await currentBackend.writeFile(currentFile.path, content)
    originalContent = content
    dirty.value = false
    ElMessage.success('已保存')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    saving.value = false
  }
}

async function beforeClose(done: () => void) {
  if (dirty.value) {
    const ok = await showConfirmDialog('关闭编辑器', '文件尚未保存，确定关闭？', true, '不保存并关闭')
    if (!ok) return
  }
  done()
}

function onClosed() {
  currentBackend = null
  currentFile = null
  ready.value = false
  loading.value = false
  dirty.value = false
}

defineExpose({ open })
</script>

<style scoped>
.editor-header {
  display: flex;
  align-items: center;
  gap: 8px;
}

.editor-title {
  font-weight: 600;
}

.dirty-tag {
  font-size: 12px;
  color: #e6c06c;
}

.editor-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 2px;
  margin-bottom: 6px;
}

.editor-path {
  font-family: var(--term-font);
  font-size: 12px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.editor-wrap {
  position: relative;
  height: 58vh;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  overflow: hidden;
}

.editor-loading-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: var(--overlay-bg);
  border-radius: 6px;
  z-index: 5;
  color: var(--text-secondary);
  font-size: 13px;
}
</style>
