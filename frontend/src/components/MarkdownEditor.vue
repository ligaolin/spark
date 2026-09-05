<template>
  <div ref="wrapRef" class="md-editor-wrap">
    <MdEditor
      v-model="content"
      :theme="settings.theme === 'dark' ? 'dark' : 'light'"
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
import { ref, watch } from 'vue'
import { MdEditor } from 'md-editor-v3'
import type { ToolbarNames } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import { useSettingsStore } from '../stores/settings'

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
const settings = useSettingsStore()

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

defineExpose({ setContent, getContent, focus, jumpToLine, setReadonly: (v: boolean) => (readonly.value = v) })
</script>

<style scoped>
.md-editor-wrap {
  height: 100%;
  min-height: 0;
  overflow: hidden;
  background: var(--editor-bg);
}

.md-editor-wrap :deep(.md-editor) {
  border-radius: 0;
  /* 根背景变量：md-editor-v3 用 --md-bk-color 作为编辑器根背景（含全屏时） */
  --md-bk-color: var(--editor-bg);
  --md-color: var(--text-primary);
  --md-hover-color: var(--active-text);
  --md-border-color: var(--border-color);
  --md-background: var(--editor-bg);
  --md-background-color: var(--editor-bg);
  --md-box-shadow: none;
  --md-input-bg-color: var(--editor-bg);
}

/* 全屏时强制铺满视口、背景不透明，避免底下内容透出 */
.md-editor-wrap :deep(.md-editor-fullscreen) {
  background-color: var(--editor-bg) !important;
  z-index: 10000;
}

.md-editor-wrap :deep(.md-editor-toolbar) {
  border-bottom: 1px solid var(--border-color);
}

.md-editor-wrap :deep(.md-editor-content) {
  font-size: 13.5px;
}
</style>
