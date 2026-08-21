<template>
  <div ref="wrapRef" class="md-editor-wrap">
    <MdEditor
      v-model="content"
      theme="dark"
      language="zh-CN"
      :preview="true"
      :toolbars="toolbars"
      :footers="['markdownTotal']"
      :no-upload-img="true"
      :read-only="readonly"
      style="height: 100%"
      @on-change="onChange"
      @on-save="emit('save')"
    />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { MdEditor } from 'md-editor-v3'
import type { ToolbarNames } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'

const props = defineProps<{
  // 是否只读（默认可编辑）
  readonly?: boolean
}>()

const emit = defineEmits<{
  (e: 'change', value: string): void
  (e: 'save'): void
}>()

const wrapRef = ref<HTMLElement>()
const content = ref('')

// 常用排版工具栏（隐藏图片上传等需要后端的项）
const toolbars: ToolbarNames[] = [
  'bold',
  'underline',
  'italic',
  'strikeThrough',
  '-',
  'title',
  'quote',
  'unorderedList',
  'orderedList',
  'task',
  '-',
  'codeRow',
  'code',
  'link',
  'table',
  '-',
  'revoke',
  'next',
  'prettier',
  '-',
  'preview',
  'previewOnly',
  'catalog',
  'fullscreen',
]

function setContent(v: string) {
  if (content.value === v) return
  content.value = v ?? ''
}

function getContent(): string {
  return content.value
}

function focus() {
  const el = wrapRef.value?.querySelector<HTMLElement>('.md-editor, textarea, [contenteditable]')
  el?.focus()
}

// 与 CodeEditor 对齐的空实现（Markdown 编辑器不支持跳行）
function jumpToLine() {}

// 与 CodeEditor 对齐：只读切换
const readonly = ref(false)
watch(
  () => props.readonly,
  (v) => {
    readonly.value = !!v
  },
  { immediate: true },
)

function onChange(v: string) {
  emit('change', v)
}

// Ctrl+S 兜底（编辑器焦点在内容区时快捷键可能被编辑器接管）
function onKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
    e.preventDefault()
    emit('save')
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown, true))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown, true))

defineExpose({ setContent, getContent, focus, jumpToLine, setReadonly: (v: boolean) => (readonly.value = v) })
</script>

<style scoped>
.md-editor-wrap {
  height: 100%;
  min-height: 0;
  overflow: hidden;
  background: #1a1d24;
}

.md-editor-wrap :deep(.md-editor) {
  border-radius: 0;
  --md-color: var(--text-primary);
  --md-hover-color: #7fb0ff;
  --md-border-color: var(--border-color);
  --md-background: #1a1d24;
  --md-background-color: #1a1d24;
  --md-box-shadow: none;
  --md-input-bg-color: #1a1d24;
}

.md-editor-wrap :deep(.md-editor-toolbar) {
  border-bottom: 1px solid var(--border-color);
}

.md-editor-wrap :deep(.md-editor-content) {
  font-size: 13.5px;
}
</style>
