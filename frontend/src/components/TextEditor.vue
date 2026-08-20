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
      <div ref="hostRef" class="editor-host"></div>
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
import { basicSetup } from 'codemirror'
import { EditorView, keymap } from '@codemirror/view'
import { EditorState, type Extension } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'
import { languages } from '@codemirror/language-data'
import { LanguageDescription } from '@codemirror/language'
import { showConfirmDialog } from '../utils/dialog'
import type { FileBackend } from '../utils/fileBackend'

const visible = ref(false)
const fileName = ref('')
const fullPath = ref('')
const loading = ref(false)
const ready = ref(false)
const saving = ref(false)
const dirty = ref(false)

const hostRef = ref<HTMLElement>()

let view: EditorView | null = null
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
  destroyEditor()
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
    await createEditor(content)
    if (token !== initToken) return
    if (currentLineNo) jumpTo(currentLineNo)
    ready.value = true
  } catch (e: any) {
    if (token !== initToken) return
    ElMessage.error(`打开文件失败：${e?.message || e}`)
    visible.value = false
  } finally {
    if (token === initToken) loading.value = false
  }
}

async function createEditor(content: string) {
  const host = hostRef.value
  if (!host) return
  const exts: Extension[] = [basicSetup, oneDark]
  try {
    const desc = LanguageDescription.matchFilename(languages, currentFile?.name ?? '')
    if (desc) exts.push(await desc.load())
  } catch {
    // 语言包加载失败不影响编辑，仅缺少语法高亮
  }
  exts.push(
    EditorView.updateListener.of((u) => {
      if (u.docChanged) {
        dirty.value = view?.state.doc.toString() !== originalContent
      }
    }),
    keymap.of([
      {
        key: 'Mod-s',
        run: () => {
          void save()
          return true
        },
      },
    ]),
  )
  const state = EditorState.create({ doc: content, extensions: exts })
  view = new EditorView({ state, parent: host })
}

function jumpTo(lineNo: number) {
  if (!view) return
  const n = Math.max(1, Math.min(lineNo, view.state.doc.lines))
  const line = view.state.doc.line(n)
  view.dispatch({
    selection: { anchor: line.from },
    effects: EditorView.scrollIntoView(line.from, { y: 'center' }),
  })
  view.focus()
}

async function save() {
  if (!view || !currentBackend || !currentFile) return
  const content = view.state.doc.toString()
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

function destroyEditor() {
  if (view) {
    view.destroy()
    view = null
  }
}

async function beforeClose(done: () => void) {
  if (dirty.value) {
    const ok = await showConfirmDialog('关闭编辑器', '文件尚未保存，确定关闭？', true, '不保存并关闭')
    if (!ok) return
  }
  destroyEditor()
  done()
}

function onClosed() {
  destroyEditor()
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
}

.editor-host {
  height: 58vh;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  overflow: hidden;
  background: #1a1d24;
}

.editor-host :deep(.cm-editor) {
  height: 100%;
  font-size: 13px;
}

.editor-host :deep(.cm-scroller) {
  font-family: var(--term-font, 'Consolas', 'Menlo', monospace);
  line-height: 1.6;
}

.editor-loading-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: rgba(18, 18, 24, 0.72);
  border-radius: 6px;
  z-index: 5;
  color: var(--text-secondary);
  font-size: 13px;
}
</style>
