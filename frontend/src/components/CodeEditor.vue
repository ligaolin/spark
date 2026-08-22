<template>
  <div ref="hostRef" class="code-editor-host"></div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { basicSetup } from 'codemirror'
import { EditorView, keymap } from '@codemirror/view'
import { Compartment, EditorSelection, EditorState, findClusterBreak, type Extension } from '@codemirror/state'
import { indentWithTab } from '@codemirror/commands'
import { oneDark } from '@codemirror/theme-one-dark'
import { languages } from '@codemirror/language-data'
import { LanguageDescription } from '@codemirror/language'
import { useSettingsStore } from '../stores/settings'

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

const settings = useSettingsStore()

// 亮色主题：与应用配色保持一致（暗色用 oneDark）
const lightTheme = EditorView.theme(
  {
    '&': { backgroundColor: '#ffffff', color: '#2b3040' },
    '.cm-content': { caretColor: '#2b3040' },
    '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
      backgroundColor: '#cfe0ff',
    },
    '.cm-gutters': { backgroundColor: '#f7f8fc', color: '#8a91a3', border: 'none' },
    '.cm-activeLine': { backgroundColor: '#f2f5fa' },
    '.cm-activeLineGutter': { backgroundColor: '#eef1f7', color: '#2b3040' },
    '&.cm-focused': { outline: 'none' },
    '.cm-tooltip': { backgroundColor: '#ffffff', border: '1px solid #e3e6ee' },
    '.cm-panels': { backgroundColor: '#f7f8fc', color: '#2b3040' },
    '.cm-panels.cm-panels-top': { borderBottom: '1px solid #e3e6ee' },
    '.cm-panels.cm-panels-bottom': { borderTop: '1px solid #e3e6ee' },
    '.cm-searchMatch': { backgroundColor: '#dbe7ff', outline: '1px solid #3b82f6' },
    '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: '#b3d4ff' },
    '.cm-selectionMatch': { backgroundColor: '#e3ecff' },
  },
  { dark: false },
)

let view: EditorView | null = null
const languageConf = new Compartment()
const editableConf = new Compartment()
const wrapConf = new Compartment()
const themeConf = new Compartment()
const wordSepConf = new Compartment()

// ---------- 双击选中分隔符 ----------
// 设置「编辑器选中分隔符」（设置 → 通用）：留空 = 连选（空格/换行仍会截断，
// 其余符号 _ . - 等全部可连选）；填写分隔符（如 _ 或 ._-）后，只有列出的
// 字符处截断。通过接管鼠标双击（mouseSelectionStyle）实现，语言无关、
// 可随时重配置。

type WordCat = 'space' | 'word' | 'other'

function makeSeparatorCategorizer(seps: string): (char: string) => WordCat {
  return (char) => {
    if (!/\S/.test(char)) return 'space' // 空格 / Tab / 换行永远截断，无需配置
    if (seps.includes(char)) return 'other' // 配置的分隔符截断
    return 'word' // 其余全部连选（含字母数字与 _ . - 等符号）
  }
}

// 按分类器找出 pos 所在的"词"区间（等价于 CodeMirror 内部 groupAt 的
// 分隔符版，选中的是一整段同类型字符）。
function groupAtWithCat(
  state: EditorState,
  pos: number,
  cat: (char: string) => WordCat,
  bias = 1,
) {
  const line = state.doc.lineAt(pos)
  const linePos = pos - line.from
  if (line.length === 0) return EditorSelection.cursor(pos)
  let from = linePos
  let to = linePos
  if (bias < 0) from = findClusterBreak(line.text, linePos, false)
  else to = findClusterBreak(line.text, linePos)
  const c = cat(line.text.slice(from, to))
  while (from > 0) {
    const prev = findClusterBreak(line.text, from, false)
    if (cat(line.text.slice(prev, from)) !== c) break
    from = prev
  }
  while (to < line.length) {
    const next = findClusterBreak(line.text, to)
    if (cat(line.text.slice(to, next)) !== c) break
    to = next
  }
  return EditorSelection.undirectionalRange(from + line.from, to + line.from)
}

// 只在 event.detail === 2（双击）时接管，单击 / 拖选 / 三击仍走默认行为
function wordSeparatorExtension(seps: string): Extension {
  return EditorView.mouseSelectionStyle.of((view, event) => {
    if (event.detail !== 2) return null
    const start = view.posAndSideAtCoords({ x: event.clientX, y: event.clientY }, false)
    if (start == null) return null
    const cat = makeSeparatorCategorizer(seps)
    const startRange = groupAtWithCat(view.state, start.pos, cat, start.assoc)
    let startPos = start.pos
    let startAssoc = start.assoc
    let startSel = view.state.selection
    return {
      update(u) {
        if (u.docChanged) {
          startPos = u.changes.mapPos(startPos)
          startSel = startSel.map(u.changes)
        }
      },
      get(ev, extend, multiple) {
        const cur = view.posAndSideAtCoords({ x: ev.clientX, y: ev.clientY }, false)
        if (!cur) return view.state.selection
        let range = groupAtWithCat(view.state, cur.pos, cat, cur.assoc)
        // 双击后拖拽扩展：从起点词到当前词的范围
        if (startPos !== cur.pos && !extend) {
          const from = Math.min(startRange.from, range.from)
          const to = Math.max(startRange.to, range.to)
          range =
            from < range.from
              ? EditorSelection.range(from, to, range.assoc)
              : EditorSelection.range(to, from, range.assoc)
        }
        if (extend) return startSel.replaceRange(startSel.main.extend(range.from, range.to, range.assoc))
        if (multiple) return startSel.addRange(range)
        return EditorSelection.create([range])
      },
    }
  })
}

onMounted(() => {
  const host = hostRef.value
  if (!host) return
  const state = EditorState.create({
    doc: '',
    extensions: [
      basicSetup,
      themeConf.of(settings.theme === 'dark' ? oneDark : lightTheme),
      EditorState.phrases.of(zhCNPhrases),
      languageConf.of([]),
      editableConf.of(EditorView.editable.of(true)),
      wrapConf.of(props.wrap ? EditorView.lineWrapping : []),
      wordSepConf.of(wordSeparatorExtension(settings.editorWordSeparators)),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) emit('change', u.state.doc.toString())
      }),
      keymap.of([
        // Tab 缩进 / Shift+Tab 反缩进（indentWithTab 绑定在 Mod-s 之前，
        // 输入框中按 Tab 时插入缩进，而不是把焦点移出编辑器）
        indentWithTab,
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

// 明暗主题切换后即时重配置 CodeMirror 主题
watch(
  () => settings.theme,
  (t) => {
    if (!view) return
    view.dispatch({ effects: themeConf.reconfigure(t === 'dark' ? oneDark : lightTheme) })
  },
)

// 双击选中分隔符配置变化后即时生效
watch(
  () => settings.editorWordSeparators,
  (seps) => {
    if (!view) return
    view.dispatch({ effects: wordSepConf.reconfigure(wordSeparatorExtension(seps)) })
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
  background: var(--editor-bg);
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
