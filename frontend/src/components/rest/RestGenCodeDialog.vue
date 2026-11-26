<template>
    <el-dialog :model-value="modelValue" :title="title" width="750px" destroy-on-close
        @update:model-value="onVisibleChange">
        <div class="curl-gen-toolbar">
            <span class="curl-gen-label">生成：</span>
            <el-select :model-value="language" size="small" class="curl-gen-select"
                @update:model-value="onLanguageChange">
                <el-option v-for="opt in CODE_LANGUAGES" :key="opt.value" :label="opt.label" :value="opt.value" />
            </el-select>
            <el-button size="small" class="curl-gen-copy" :icon="CopyDocument" @click="copy">复制</el-button>
        </div>
        <div class="curl-gen-editor">
            <CodeEditor ref="editorRef" :filename="filename" wrap />
        </div>
    </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import CodeEditor from '../CodeEditor.vue'
import { CODE_LANGUAGES, genDialogTitle, genEditorFilename, type CodeLanguage } from '../../utils/restCodegen'

const props = defineProps<{
    modelValue: boolean
    language: CodeLanguage
    code: string
}>()

const emit = defineEmits<{
    (e: 'update:modelValue', v: boolean): void
    (e: 'update:language', v: CodeLanguage): void
}>()

const editorRef = ref<InstanceType<typeof CodeEditor> | null>(null)

const title = computed(() => genDialogTitle(props.language))
const filename = computed(() => genEditorFilename(props.language))

function onVisibleChange(v: boolean) {
    emit('update:modelValue', v)
}

function onLanguageChange(v: CodeLanguage) {
    emit('update:language', v)
}

// 弹窗打开后（destroy-on-close 会重建编辑器）或代码/语言变化时刷新内容
watch(
    () => [props.modelValue, props.code] as const,
    async ([open, code]) => {
        if (!open || !code) return
        await nextTick()
        editorRef.value?.setContent(code)
    },
    { immediate: true },
)

function copy() {
    const text = editorRef.value?.getContent() || props.code
    navigator.clipboard.writeText(text).then(() => {
        ElMessage.success('已复制到剪贴板')
    }).catch(() => {
        ElMessage.warning('复制失败，请手动选择复制')
    })
}
</script>

<style scoped>
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
</style>