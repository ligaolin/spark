<template>
  <div ref="hostRef" class="code-editor-host"></div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { basicSetup } from 'codemirror'
import { EditorView, keymap } from '@codemirror/view'
import { Compartment, EditorState, type Extension } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'
import { languages } from '@codemirror/language-data'
import { LanguageDescription } from '@codemirror/language'

const props = defineProps<{
  // 用于根据文件扩展名自动匹配语法高亮
  filename?: string
  // 是否自动换行（长行折行显示，默认关闭）
  wrap?: boolean
}>()

const emit = defineEmits<{
  (e: 'change', value: string): void
  (e: 'save'): void
}>()

// CodeMirror 内置 UI（搜索面板、转到行、自动补全等）的英文短语 → 中文。
// 通过 EditorState.phrases 覆盖，未覆盖到的短语会回退为英文原文。
const zhCNPhrases: Record<string, string> = {
  // 搜索 / 替换面板（Ctrl+F）
  Find: '查找',
  Replace: '替换',
  next: '下一个',
  previous: '上一个',
  all: '全部',
  'match case': '区分大小写',
  regexp: '正则',
  'by word': '全词',
  replace: '替换',
  'replace all': '全部替换',
  close: '关闭',
  // 转到行
  'Go to line': '转到行',
  go: '转到',
  // 无障碍播报
  'replaced match on line $': '已替换第 $ 行的匹配',
  'replaced $ matches': '已替换 $ 处匹配',
  'current match': '当前匹配',
  'on line': '在第',
  'Control character': '控制字符',
  // 自动补全
  Completions: '补全',
}

const hostRef = ref<HTMLElement>()

let view: EditorView | null = null
const languageConf = new Compartment()
const editableConf = new Compartment()
const wrapConf = new Compartment()

onMounted(() => {
  const host = hostRef.value
  if (!host) return
  const state = EditorState.create({
    doc: '',
    extensions: [
      basicSetup,
      oneDark,
      EditorState.phrases.of(zhCNPhrases),
      languageConf.of([]),
      editableConf.of(EditorView.editable.of(true)),
      wrapConf.of(props.wrap ? EditorView.lineWrapping : []),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) emit('change', u.state.doc.toString())
      }),
      keymap.of([
        {
          key: 'Mod-s',
          run: () => {
            emit('save')
            return true
          },
        },
      ]),
    ],
  })
  view = new EditorView({ state, parent: host })
  void applyLanguage(props.filename)
})

watch(
  () => props.filename,
  (f) => void applyLanguage(f),
)

watch(
  () => props.wrap,
  (w) => {
    if (!view) return
    view.dispatch({ effects: wrapConf.reconfigure(w ? EditorView.lineWrapping : []) })
  },
)

async function applyLanguage(filename?: string) {
  if (!view) return
  let support: Extension = []
  try {
    const desc = LanguageDescription.matchFilename(languages, filename ?? '')
    if (desc) support = await desc.load()
  } catch {
    // 语言包加载失败不影响编辑，仅缺少语法高亮
  }
  view.dispatch({ effects: languageConf.reconfigure(support) })
}

function setContent(content: string) {
  if (!view) return
  view.dispatch({
    changes: { from: 0, to: view.state.doc.length, insert: content },
  })
}

function getContent(): string {
  return view?.state.doc.toString() ?? ''
}

function focus() {
  view?.focus()
}

function jumpToLine(n: number) {
  if (!view) return
  const lineNo = Math.max(1, Math.min(n, view.state.doc.lines))
  const line = view.state.doc.line(lineNo)
  view.dispatch({
    selection: { anchor: line.from },
    effects: EditorView.scrollIntoView(line.from, { y: 'center' }),
  })
  view.focus()
}

function setReadonly(readonly: boolean) {
  if (!view) return
  view.dispatch({
    effects: editableConf.reconfigure(EditorView.editable.of(!readonly)),
  })
}

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})

defineExpose({ setContent, getContent, focus, jumpToLine, setReadonly })
</script>

<style scoped>
.code-editor-host {
  height: 100%;
  min-height: 0;
  overflow: hidden;
  background: #1a1d24;
}

.code-editor-host :deep(.cm-editor) {
  height: 100%;
  font-size: 13px;
  outline: none;
}

.code-editor-host :deep(.cm-scroller) {
  font-family: var(--term-font, 'Consolas', 'Menlo', monospace);
  line-height: 1.6;
}
</style>
