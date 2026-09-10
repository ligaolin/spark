<template>
  <el-form ref="formRef" :model="form" label-width="120px" label-position="left" class="ai-form">
    <el-form-item label="服务商预设">
      <el-select v-model="provider" style="width: 260px" @change="applyPreset">
        <el-option
          v-for="p in AI_PROVIDERS"
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
      <el-select v-model="form.model" filterable allow-create default-first-option
        placeholder="选择或输入模型" style="width: 300px">
        <el-option v-for="m in modelOptions" :key="m" :label="m" :value="m" />
      </el-select>
      <el-button size="small" text :loading="loadingModels" @click="loadModels">刷新</el-button>
      <div class="ai-note-inline">从服务商接口获取，也可手输</div>
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

    <el-divider content-position="left">终端 Agent（SSH 终端的 AI 面板）</el-divider>

    <el-form-item label="允许命令串联">
      <el-switch v-model="allowChain" :loading="savingAllowChain" @change="saveAllowChain" />
      <div class="ai-note-inline">
        开启后 AI 可以在一条命令里用 <span class="mono-inline">&&</span> /
        <span class="mono-inline">;</span> 串联多个步骤，多步操作更利索——代价是「完全授权」模式下一次批准可能执行一连串命令。
        关闭时一次只执行一条命令（工作目录仍会自动延续，不需要它写 cd 前缀）。保存后下一轮对话生效
      </div>
    </el-form-item>

    <el-form-item label="附带终端上下文">
      <el-switch v-model="terminalContext" :loading="savingTerminalContext" @change="saveTerminalContext" />
      <div class="ai-note-inline">
        把你在 SSH 终端窗口里最近看到的输出（含刚敲的命令与报错）一起发给模型，这样「帮我看看这个报错」才答得准。
        终端里有敏感内容时可关闭。保存后下一轮对话生效
      </div>
    </el-form-item>

    <el-form-item>
      <div class="ai-actions">
        <el-button type="primary" :loading="saving" @click="save">保存模型配置</el-button>
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
import { AI_PROVIDERS, providerForBaseUrl } from '../utils/aiProviders'
import { useSettingsStore } from '../stores/settings'

const form = reactive<AIConfig>(makeAIConfig())
const apiKey = ref('')
const provider = ref<string>('custom')
const saving = ref(false)
const clearing = ref(false)
const modelOptions = ref<string[]>([])
const loadingModels = ref(false)

// 终端 Agent 的两个开关存在通用的 settings 表里（key: ai.agent.*）
const settings = useSettingsStore()
const allowChain = ref(false)
const terminalContext = ref(true)
const savingAllowChain = ref(false)
const savingTerminalContext = ref(false)

async function saveAllowChain(v: string | number | boolean) {
  savingAllowChain.value = true
  try {
    await settings.set('ai.agent.allowChain', v ? '1' : '0')
    ElMessage.success(v ? '已保存：允许 AI 串联多条命令' : '已保存：AI 一次只执行一条命令')
  } catch (e: any) {
    allowChain.value = !v
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingAllowChain.value = false
  }
}

async function saveTerminalContext(v: string | number | boolean) {
  savingTerminalContext.value = true
  try {
    await settings.set('ai.agent.terminalContext', v ? '1' : '0')
    ElMessage.success(v ? '已保存：AI 会读取终端最近输出' : '已保存：AI 不再读取终端输出')
  } catch (e: any) {
    terminalContext.value = !v
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingTerminalContext.value = false
  }
}

// 从供应商 /v1/models 接口拉取可用模型列表（不内置）。
async function loadModels() {
  loadingModels.value = true
  try {
    const ids = await AIService.ListModels()
    modelOptions.value = ids && ids.length ? ids : []
    if (!ids || !ids.length) {
      ElMessage.info('服务商未返回可用模型，可手动输入模型名称')
    }
  } catch (e: any) {
    modelOptions.value = []
    ElMessage.error(`获取模型列表失败：${e?.message || e}`)
  } finally {
    loadingModels.value = false
  }
}

function applyPreset(key: string) {
  const p = AI_PROVIDERS.find((x) => x.key === key)
  if (p) {
    form.baseUrl = p.baseUrl
    form.model = p.model
  }
  void loadModels()
}

async function load() {
  try {
    const cfg = await AIService.GetConfig()
    Object.assign(form, cfg)
    // 根据保存的 baseUrl 反推服务商预设，避免每次进入都显示「自定义」
    const matched = providerForBaseUrl(cfg.baseUrl)
    provider.value = matched ? matched.key : 'custom'
    void loadModels()
  } catch {
    /* 忽略，保留默认值 */
  }
  await settings.load()
  allowChain.value = settings.values['ai.agent.allowChain'] === '1'
  terminalContext.value = settings.values['ai.agent.terminalContext'] !== '0'
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
    void loadModels()
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

.mono-inline {
  font-family: var(--term-font);
  background: var(--hover-bg);
  border-radius: 3px;
  padding: 0 3px;
}

.ai-actions {
  display: flex;
  gap: 8px;
}
</style>
