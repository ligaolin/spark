<template>
    <el-dialog :model-value="modelValue" :title="title" width="86%" top="4vh" destroy-on-close class="file-preview-dialog"
        @update:model-value="emit('update:modelValue', $event)">
        <div class="preview-body">
            <img v-if="kind === 'image'" :src="src" :alt="title" class="preview-img" />
            <iframe v-else-if="kind === 'pdf'" :src="src" class="preview-frame" />
        </div>
        <template #footer>
            <span v-if="kind === 'pdf'" class="preview-hint">若 PDF 无法显示，请下载后用系统程序打开</span>
            <el-button @click="emit('update:modelValue', false)">关闭</el-button>
        </template>
    </el-dialog>
</template>

<script setup lang="ts">
defineProps<{
    modelValue: boolean
    title: string
    src: string
    kind: 'image' | 'pdf'
}>()

const emit = defineEmits<{
    (e: 'update:modelValue', v: boolean): void
}>()
</script>

<style scoped>
.preview-body {
    height: 72vh;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: auto;
    background: var(--bg-secondary);
    border-radius: 4px;
}

.preview-img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
}

.preview-frame {
    width: 100%;
    height: 100%;
    border: none;
    background: #fff;
}

.preview-hint {
    float: left;
    font-size: 12px;
    color: var(--text-muted);
    line-height: 32px;
}
</style>