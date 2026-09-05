<template>
  <el-form ref="formRef" :model="form" label-width="120px" label-position="left" class="ai-form">
    <el-form-item label="服务商预设">
      <el-select v-model="provider" style="width: 260px" @change="applyPreset">
        <el-option
          v-for="p in PRESETS"
          :key="p.key"
          :value="p.key"
          :label="p.label"
        />
      </el-select>
      <div class="ai-note-inline">选一个预设会自动填好地址与模型，选「自定义」则手动填写</div>
    </el-form-item>

    <el-form-item label="API 地址">
      <el-input
        v-model="form.baseUrl"
        placeholder="https://api.openai.com/v1"
        style="width: 360px"
      />
      <div class="ai-note-inline">OpenAI 兼容协议的服务地址（以 /v1 结尾，不含 /chat/completions）</div>
    </el-form-item>

    <el-form-item label="模型名称">
      <el-input v-model="form.model" placeholder="gpt-4o-mini" style="width: 360px" />
    </el-form-item>

    <el-form-item label="API Key">
      <el-input
        v-model="apiKey"
        type="password"
        show-password
        :placeholder="form.hasKey ? '已配置（留空表示保持不变）' : '请输入 API Key'"
        style="width: 360px"
      />
      <div class="ai-note-inline">密钥加密保存在本地数据库，只用于调用你填写的服务商</div>
    </el-form-item>

    <el-form-item label="温度">
      <el-input-number
        v-model="form.temperature"
        :min="0"
        :max="2"
        :step="0.1"
        :precision="1"
        style="width: 140px"
      />
      <div class="ai-note-inline">越低越稳定，越高越发散</div>
    </el-form-item>

    <el-form-item label="最大输出">
      <el-input-number
        v-model="form.maxTokens"
        :min="0"
        :max="131072"
        :step="256"
        style="width: 160px"
      />
      <div class="ai-note-inline">单次回复的最大 token 数，0 表示不限制（交给服务商默认值）</div>
    </el-form-item>

    <el-form-item label="系统提示词">
      <el-input
        v-model="form.systemPrompt"
        type="textarea"
        :autosize="{ minRows: 2, maxRows: 6 }"
        style="width: 560px"
      />
    </el-form-item>

    <el-form-item label=" ">
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="隐私提示"
        description="你发送的消息会发送到你配置的模型服务商服务器。请勿把密码、私钥、主机名等敏感信息发给模型。"
      />
    </el-form-item>

    <el-form-item>
      <div class="ai-actions">
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
        <el-button v-if="form.hasKey" :loading="clearing" @click="clearKey">清除 Key</el-button>
      </div>
    </el-form-item>
  </el-form>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { AIService, makeAIConfig, type AIConfig } from '../utils/wails'
import { showConfirmDialog } from '../utils/dialog'

const PRESETS = [
  { key: 'openai', label: 'OpenAI', baseUrl: 'https://api.openai.com/v1', model: 'gpt-4o-mini' },
  { key: 'deepseek', label: 'DeepSeek', baseUrl: 'https://api.deepseek.com/v1', model: 'deepseek-chat' },
  { key: 'qwen', label: '通义千问（阿里云）', baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', model: 'qwen-plus' },
  { key: 'zhipu', label: '智谱 AI', baseUrl: 'https://open.bigmodel.cn/api/paas/v4', model: 'glm-4-flash' },
  { key: 'custom', label: '自定义', baseUrl: '', model: '' },
] as const

const form = reactive<AIConfig>(makeAIConfig())
const apiKey = ref('')
const provider = ref<string>('custom')
const saving = ref(false)
const clearing = ref(false)

function applyPreset(key: string) {
  const p = PRESETS.find((x) => x.key === key)
  if (p) {
    form.baseUrl = p.baseUrl
    form.model = p.model
  }
}

async function load() {
  try {
    const cfg = await AIService.GetConfig()
    Object.assign(form, cfg)
    // 根据保存的 baseUrl 反推服务商预设，避免每次进入都显示「自定义」
    const curBase = (cfg.baseUrl || '').replace(/\/+$/, '')
    const matched = PRESETS.find((p) => p.baseUrl && p.baseUrl.replace(/\/+$/, '') === curBase)
    provider.value = matched ? matched.key : 'custom'
  } catch {
    /* 忽略，保留默认值 */
  }
}

async function save() {
  if (!form.baseUrl.trim()) {
    ElMessage.warning('请填写 API 地址')
    return
  }
  if (!form.model.trim()) {
    ElMessage.warning('请填写模型名称')
    return
  }
  saving.value = true
  try {
    await AIService.SaveConfig({ ...form, baseUrl: form.baseUrl.trim(), model: form.model.trim() }, apiKey.value)
    form.hasKey = true
    apiKey.value = ''
    ElMessage.success('已保存')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    saving.value = false
  }
}

async function clearKey() {
  const ok = await showConfirmDialog('清除 API Key', '确定清除已保存的 API Key？清除后 AI 助手将无法发送消息。', false, '清除')
  if (!ok) return
  clearing.value = true
  try {
    await AIService.ClearKey()
    form.hasKey = false
    ElMessage.success('已清除')
  } catch (e: any) {
    ElMessage.error(`清除失败：${e?.message || e}`)
  } finally {
    clearing.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.ai-form {
  --el-form-label-font-size: 13px;
}

.ai-form :deep(.el-form-item__label) {
  color: var(--text-primary);
  font-weight: 500;
}

.ai-note-inline {
  font-size: 12px;
  color: var(--text-secondary);
  margin-left: 10px;
}

.ai-actions {
  display: flex;
  gap: 8px;
}
</style>
