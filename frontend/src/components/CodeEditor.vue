<template>
  <div ref="hostRef" class="code-editor-host"></div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { editor as MonacoEditor } from 'monaco-editor'
import { useSettingsStore } from '../stores/settings'
import { loadMonaco, monacoTheme, type Monaco } from '../utils/monaco'
import { registerTextMateGrammars } from '../utils/textmate'

const props = defineProps<{
  // 用于根据文件扩展名自动匹配语法高亮
  filename?: string
  // 是否自动换行(长行折行显示,默认关闭)
  wrap?: boolean
}>()

const emit = defineEmits<{
  (e: 'change', value: string): void
  (e: 'save'): void
}>()

const hostRef = ref<HTMLElement>()

const settings = useSettingsStore()

let monaco: Monaco | null = null
let editor: MonacoEditor.IStandaloneCodeEditor | null = null
let suppressChange = false
// Monaco 是懒加载的,编辑器创建前调用方可能已 setContent / jumpToLine /
// focus(旧版 CodeMirror 同步创建所以没这个问题),这里先缓存、就绪后回放
let pendingContent: string | null = null
let pendingLine: number | undefined
let pendingFocus = false

// 常见的无扩展名点文件 → 语言映射(其余按扩展名匹配 Monaco 已注册语言)
const dotfileLanguages: Record<string, string> = {
  '.bashrc': 'shell',
  '.bash_profile': 'shell',
  '.bash_aliases': 'shell',
  '.bash_functions': 'shell',
  '.profile': 'shell',
  '.zshrc': 'shell',
  '.zprofile': 'shell',
  '.gitconfig': 'ini',
  '.editorconfig': 'ini',
}

function languageIdForFile(filename?: string): string {
  if (!monaco || !filename) return 'plaintext'
  const base = filename.toLowerCase()
  if (dotfileLanguages[base]) return dotfileLanguages[base]
  const ext = /(\.[^.]+)$/.exec(base)?.[1] ?? ''
  if (!ext) return 'plaintext'
  const langs = monaco.languages.getLanguages()
  for (const l of langs) {
    if (l.filenames?.includes(base)) return l.id
  }
  for (const l of langs) {
    if (l.extensions?.includes(ext)) return l.id
  }
  return 'plaintext'
}

onMounted(async () => {
  const host = hostRef.value
  if (!host) return

  // 首次打开编辑器时才加载 Monaco;不等 TextMate,先建编辑器、先填内容
  monaco = await loadMonaco()

  const fontFamily =
    getComputedStyle(host).getPropertyValue('--term-font').trim() || undefined

  editor = monaco.editor.create(host, {
    model: monaco.editor.createModel('', languageIdForFile(props.filename)),
    theme: monacoTheme(settings.theme === 'dark'),
    automaticLayout: true,
    fontSize: 13,
    fontFamily,
    wordWrap: props.wrap ? 'on' : 'off',
    // 双击/词导航的分隔符,与应用「编辑器选中分隔符」设置保持一致
    wordSeparators: settings.editorWordSeparators,
    minimap: { enabled: true },
    scrollBeyondLastLine: false,
    smoothScrolling: true,
    padding: { top: 4, bottom: 4 },
  })

  // Ctrl+S / Cmd+S 保存
  editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => emit('save'))

  editor.onDidChangeModelContent(() => {
    if (!suppressChange) emit('change', editor!.getValue())
  })

  // 回放在编辑器就绪前积压的操作
  if (pendingContent != null) {
    suppressChange = true
    editor.setValue(pendingContent)
    suppressChange = false
    pendingContent = null
  }
  if (pendingLine != null) {
    jumpToLine(pendingLine)
    pendingLine = undefined
  } else if (pendingFocus) {
    editor.focus()
    pendingFocus = false
  }

  // TextMate 语法(vue/toml/nginx)注册是异步的,完成后重新匹配一次语言,
  // 让这些语言从 plaintext 升级为对应语法高亮
  registerTextMateGrammars(monaco)
    .catch(() => {})
    .then(() => {
      const model = editor?.getModel()
      if (monaco && model) monaco.editor.setModelLanguage(model, languageIdForFile(props.filename))
    })
})

watch(
  () => props.filename,
  (f) => {
    const model = editor?.getModel()
    if (monaco && model) monaco.editor.setModelLanguage(model, languageIdForFile(f))
  },
)

watch(
  () => props.wrap,
  (w) => editor?.updateOptions({ wordWrap: w ? 'on' : 'off' }),
)

// 明暗主题切换后即时切换 Monaco 主题
watch(
  () => settings.theme,
  (t) => {
    if (monaco) monaco.editor.setTheme(monacoTheme(t === 'dark'))
  },
)

// 双击选中分隔符配置变化后即时生效
watch(
  () => settings.editorWordSeparators,
  (seps) => editor?.updateOptions({ wordSeparators: seps }),
)

function setContent(content: string) {
  if (!editor) {
    // 编辑器尚未就绪(Monaco 懒加载中),缓存待回放
    pendingContent = content
    return
  }
  if (editor.getValue() === content) return
  suppressChange = true
  editor.setValue(content)
  suppressChange = false
}

function getContent(): string {
  return editor?.getValue() ?? pendingContent ?? ''
}

function focus() {
  if (!editor) {
    pendingFocus = true
    return
  }
  editor.focus()
}

function jumpToLine(n?: number) {
  if (!editor) {
    if (n && n > 0) pendingLine = n
    pendingFocus = true
    return
  }
  if (n && n > 0) {
    const line = Math.min(n, editor.getModel()?.getLineCount() ?? n)
    editor.revealLineInCenter(line)
    editor.setPosition({ lineNumber: line, column: 1 })
  }
  editor.focus()
}

function setReadonly(readonly: boolean) {
  // 编辑器就绪前的只读设置会在创建时由调用方状态覆盖,这里直接等就绪
  editor?.updateOptions({ readOnly: readonly })
}

onBeforeUnmount(() => {
  const model = editor?.getModel()
  editor?.dispose()
  editor = null
  model?.dispose()
  monaco = null
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

.code-editor-host :deep(.monaco-editor) {
  font-size: 13px;
}
</style>
