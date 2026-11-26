<template>
    <el-dialog :model-value="modelValue" title="导入 curl" width="650px" destroy-on-close
        @update:model-value="onVisibleChange">
        <el-input v-model="curlText" type="textarea" :rows="10"
            placeholder="粘贴 curl 命令，例如：&#10;curl -X POST https://api.example.com/users \&#10;  -H 'Content-Type: application/json' \&#10;  -d '{&quot;name&quot;: &quot;test&quot;}'" />
        <template #footer>
            <el-button @click="emit('update:modelValue', false)">取消</el-button>
            <el-button type="primary" @click="submit">解析并导入</el-button>
        </template>
    </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { parseCurl, type CurlParseResult } from '../../utils/restCurl'

const props = defineProps<{
    modelValue: boolean
}>()

const emit = defineEmits<{
    (e: 'update:modelValue', v: boolean): void
    (e: 'import', parsed: CurlParseResult): void
}>()

const curlText = ref('')

function onVisibleChange(v: boolean) {
    emit('update:modelValue', v)
}

// 每次打开都清空，避免带上一次的残留
watch(
    () => props.modelValue,
    (v) => {
        if (v) curlText.value = ''
    },
)

function submit() {
    const cmd = curlText.value.trim()
    if (!cmd) {
        ElMessage.warning('请粘贴 curl 命令')
        return
    }
    try {
        emit('import', parseCurl(cmd))
    } catch (e: any) {
        ElMessage.error('解析失败: ' + (e?.message || e))
    }
}
</script>