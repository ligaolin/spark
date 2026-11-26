<template>
  <el-form ref="formRef" :model="form" label-width="120px" label-position="left" class="ai-form">
    <el-form-item label="接口配置">
      <el-select v-model="editingId" style="width: 300px" placeholder="选择或新建接口" @change="onProviderChange">
        <el-option v-for="p in providers" :key="p.id" :value="p.id"
          :label="p.active ? `${p.name}（当前使用）` : p.name" />
        <el-option :value="0" label="＋ 新建接口" />
      </el-select>
      <el-button size="small" :disabled="!editingId || editingActive" :loading="activating" @click="activate">
        切换到此接口
      </el-button>
      <div class="ai-note-inline">
        可以保存多个 AI 接口，用哪个就切到哪个；切换后所有 AI 功能立即使用当前接口
      </div>
    </el-form-item>

    <el-form-item label="接口名称">
      <el-input v-model="providerName" placeholder="例如 DeepSeek / 公司网关" style="width: 300px" />
      <div class="ai-note-inline">仅用于在列表里区分，可留空（自动取模型名）</div>
    </el-form-item>

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
      <div class="ai-note-inline">
        {{ modelHint || '从服务商接口获取，也可直接手输（没有 /models 接口的服务商，输入模型名即可）' }}
      </div>
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

    <el-form-item label=" ">
      <div class="ai-note-inline" style="margin-left: 0">
        终端 Agent 依赖模型的工具调用（function calling）能力；服务商没有 /models 接口不影响使用，在「模型名称」里直接输入即可。
      </div>
    </el-form-item>

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

    <el-form-item label="步数用尽自动续跑">
      <el-switch v-model="autoContinue" :loading="savingAutoContinue" @change="saveAutoContinue" />
      <div class="ai-note-inline">
        单轮最多 40 步；开启后如果一直在正常推进（有成功命令、没有连续失败或重复同一条命令），AI 会自动接着做，
        最多再续 3 段（160 步）。打转时会自动停下，不会无限刷命令。关闭则到 40 步即停、需要你追问「继续」。保存后下一轮对话生效
      </div>
    </el-form-item>

    <el-form-item>
      <div class="ai-actions">
        <el-button type="primary" :loading="saving" @click="save">
          {{ editingId ? '保存接口' : '保存为新接口' }}
        </el-button>
        <el-button v-if="editingId && !editingActive" :loading="activating" @click="activate">切换到此接口</el-button>
        <el-button v-if="editingId" :loading="deleting" @click="removeProvider">删除接口</el-button>
        <el-button v-if="form.hasKey" :loading="clearing" @click="clearKey">清除 Key</el-button>
      </div>
    </el-form-item>
  </el-form>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { AIService, makeAIConfig, type AIConfig, type AIProviderInfo } from '../utils/wails'
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
// 模型列表拉取失败不算错误：不少 OpenAI 兼容服务没有 /models 接口，手输模型名即可
const modelHint = ref('')

// 多接口：列表里选一条编辑；editingId 为 0 表示新建
const providers = ref<AIProviderInfo[]>([])
const editingId = ref(0)
const providerName = ref('')
const activating = ref(false)
const deleting = ref(false)
const editingActive = computed(() =>
  editingId.value !== 0 && providers.value.some((p) => p.id === editingId.value && p.active),
)

// 终端 Agent 的开关存在通用的 settings 表里（key: ai.agent.*）
const settings = useSettingsStore()
const allowChain = ref(false)
const terminalContext = ref(true)
const autoContinue = ref(true)
const savingAllowChain = ref(false)
const savingTerminalContext = ref(false)
const savingAutoContinue = ref(false)

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

async function saveAutoContinue(v: string | number | boolean) {
  savingAutoContinue.value = true
  try {
    await settings.set('ai.agent.autoContinue', v ? '1' : '0')
    ElMessage.success(v ? '已保存：步数用尽时会自动接着做' : '已保存：步数用尽即停，需追问继续')
  } catch (e: any) {
    autoContinue.value = !v
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingAutoContinue.value = false
  }
}

// 从该接口自己的 /v1/models 接口拉取可用模型列表（不内置）。
// 失败或为空都不阻塞使用：模型名允许手动输入。
async function loadModels() {
  if (!editingId.value) {
    // 新接口还没保存，拿不到它的密钥；保存后再刷新即可
    modelOptions.value = []
    modelHint.value = ''
    return
  }
  loadingModels.value = true
  try {
    const ids = await AIService.ListModelsForProvider(editingId.value)
    modelOptions.value = ids && ids.length ? ids : []
    modelHint.value = ids && ids.length ? '' : '服务商未返回模型列表，可直接手动输入模型名称'
  } catch (e: any) {
    modelOptions.value = []
    const detail = String(e?.message || e).slice(0, 120)
    modelHint.value = `服务商未提供模型列表（${detail}），可直接手动输入模型名称`
  } finally {
    loadingModels.value = false
  }
}

function blankForm() {
  Object.assign(form, makeAIConfig({ baseUrl: '', model: '' }))
  providerName.value = ''
  apiKey.value = ''
  provider.value = 'custom'
  modelOptions.value = []
  modelHint.value = ''
}

function applyProvider(p: AIProviderInfo) {
  Object.assign(form, {
    baseUrl: p.baseUrl,
    model: p.model,
    temperature: p.temperature,
    maxTokens: p.maxTokens,
    systemPrompt: p.systemPrompt,
    hasKey: p.hasKey,
  })
  providerName.value = p.name
  apiKey.value = ''
  const matched = providerForBaseUrl(p.baseUrl)
  provider.value = matched ? matched.key : 'custom'
  void loadModels()
}

function onProviderChange(id: number) {
  if (!id) {
    blankForm()
    return
  }
  const p = providers.value.find((x) => x.id === id)
  if (p) applyProvider(p)
}

async function loadProviders(selectId?: number) {
  try {
    providers.value = (await AIService.ListProviders()) || []
  } catch (e: any) {
    providers.value = []
    ElMessage.error(`读取接口列表失败：${e?.message || e}`)
    return
  }
  const fallback = providers.value.find((p) => p.active)?.id ?? providers.value[0]?.id ?? 0
  const target = selectId && providers.value.some((p) => p.id === selectId) ? selectId : fallback
  editingId.value = target
  onProviderChange(target)
}

async function activate() {
  if (!editingId.value) return
  activating.value = true
  try {
    await AIService.ActivateProvider(editingId.value)
    await loadProviders(editingId.value)
    ElMessage.success('已切换，所有 AI 功能将使用这个接口')
  } catch (e: any) {
    ElMessage.error(`切换失败：${e?.message || e}`)
  } finally {
    activating.value = false
  }
}

async function removeProvider() {
  if (!editingId.value) return
  const p = providers.value.find((x) => x.id === editingId.value)
  const ok = await showConfirmDialog('删除接口', `确定删除「${p?.name || '该接口'}」？`, true, '删除')
  if (!ok) return
  deleting.value = true
  try {
    await AIService.DeleteProvider(editingId.value)
    await loadProviders()
    ElMessage.success('已删除')
  } catch (e: any) {
    ElMessage.error(`删除失败：${e?.message || e}`)
  } finally {
    deleting.value = false
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
  await settings.load()
  allowChain.value = settings.values['ai.agent.allowChain'] === '1'
  terminalContext.value = settings.values['ai.agent.terminalContext'] !== '0'
  autoContinue.value = settings.values['ai.agent.autoContinue'] !== '0'
  await loadProviders()
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
    const isNew = editingId.value === 0
    const wasActive = editingActive.value
    const id = await AIService.SaveProvider({
      id: editingId.value,
      name: providerName.value.trim(),
      baseUrl: form.baseUrl.trim(),
      model: form.model.trim(),
      temperature: form.temperature,
      maxTokens: form.maxTokens,
      systemPrompt: form.systemPrompt,
      hasKey: false,
      active: false,
    }, apiKey.value)
    // 新建的接口直接启用；编辑当前接口时同步生效
    if (isNew || wasActive) {
      await AIService.ActivateProvider(id)
    }
    apiKey.value = ''
    await loadProviders(id)
    ElMessage.success(isNew || wasActive ? '已保存并启用' : '已保存')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    saving.value = false
  }
}

async function clearKey() {
  const ok = await showConfirmDialog('清除 API Key', '确定清除该接口已保存的 API Key？清除后 AI 助手将无法发送消息。', false, '清除')
  if (!ok) return
  clearing.value = true
  try {
    await AIService.ClearProviderKey(editingId.value)
    form.hasKey = false
    await loadProviders(editingId.value)
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
