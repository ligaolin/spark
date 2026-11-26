<template>
  <el-dialog
    :model-value="modelValue"
    :title="dialogTitle"
    width="520px"
    :close-on-click-modal="false"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <el-form ref="formRef" :model="form" :rules="rules" label-width="86px" label-position="left">
      <el-form-item label="连接名称" prop="name" v-if="mode !== 'connect'">
        <el-input v-model="form.name" placeholder="给这个连接起个名字" />
      </el-form-item>

      <el-form-item label="分组" v-if="mode !== 'connect'">
        <el-select
          v-model="form.group"
          filterable
          allow-create
          clearable
          default-first-option
          placeholder="选择或输入分组名（可留空）"
          style="width: 100%"
        >
          <el-option v-for="g in groups" :key="g" :label="g" :value="g" />
        </el-select>
      </el-form-item>

      <el-form-item label="类型" prop="type" v-if="mode === 'create'">
        <el-radio-group v-model="form.type">
          <el-radio value="ssh">SSH</el-radio>
          <el-radio value="ftp">FTP</el-radio>
        </el-radio-group>
      </el-form-item>

      <div class="row">
        <el-form-item label="主机" prop="host" class="row-host">
          <el-input v-model="form.host" placeholder="IP 或域名" />
        </el-form-item>
        <el-form-item label="端口" prop="port" class="row-port" label-width="50px" style="margin-left: 20px;">
          <el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" />
        </el-form-item>
      </div>

      <el-form-item label="用户名" prop="username">
        <el-input v-model="form.username" placeholder="登录用户名" />
      </el-form-item>

      <el-form-item label="密码" prop="password">
        <el-input v-model="form.password" type="password" show-password placeholder="登录密码" />
      </el-form-item>

      <el-form-item label="认证方式">
        <el-switch v-model="form.useKey" active-text="私钥认证" inactive-text="密码认证" />
      </el-form-item>

      <template v-if="form.useKey">
        <el-form-item label="密钥来源">
          <el-radio-group v-model="keySource">
            <el-radio value="store">密钥库</el-radio>
            <el-radio value="manual">手动粘贴</el-radio>
          </el-radio-group>
        </el-form-item>
        <template v-if="keySource === 'store'">
          <el-form-item label="选择密钥">
            <el-select
              v-model="form.keyId"
              filterable
              :loading="loadingKeys"
              placeholder="选择密钥库中的 SSH 密钥"
              style="width: 100%"
            >
              <el-option
                v-for="k in managedKeys"
                :key="k.id"
                :value="k.id"
                :label="`${k.name}（${k.type}${k.fingerprint ? ' · ' + k.fingerprint.slice(0, 16) : ''}）`"
              />
            </el-select>
            <div class="field-tip">
              {{ managedKeys.length
                ? '引用密钥库中的密钥，私钥与口令随密钥库统一维护；连接只保存引用'
                : '密钥库为空，请先到「设置 → SSH 密钥」生成或导入' }}
            </div>
          </el-form-item>
        </template>
        <template v-else>
          <el-form-item label="私钥文件">
            <el-input
              v-model="keyPath"
              placeholder="私钥文件路径，如 ~/.ssh/id_ed25519"
              @blur="loadKeyPath"
              @keyup.enter="loadKeyPath"
            >
              <template #append>
                <el-button :loading="loadingKey" @click="pickKeyFile">选择文件</el-button>
              </template>
            </el-input>
            <div class="field-tip">选文件或填路径后自动读取到下方；保存的是内容，之后移动原文件不影响连接</div>
          </el-form-item>
          <el-form-item label="私钥内容" prop="privateKey">
            <el-input
              v-model="form.privateKey"
              type="textarea"
              :rows="5"
              placeholder="粘贴 PEM 格式私钥内容（RSA / EC / OPENSSH），或用上面的文件路径自动读取"
            />
          </el-form-item>
          <el-form-item label="密钥口令">
            <el-input v-model="form.passphrase" type="password" show-password placeholder="加密私钥的口令（可选）" />
          </el-form-item>
        </template>
      </template>

      <el-form-item label="跳板机" v-if="form.type === 'ssh' && jumpOptions.length">
        <el-select v-model="form.jumpId" clearable placeholder="直连（不使用跳板机）" style="width: 100%">
          <el-option v-for="c in jumpOptions" :key="c.id" :value="c.id"
            :label="`${c.name}（${c.username}@${c.host}${c.port && c.port !== 22 ? ':' + c.port : ''}）`" />
        </el-select>
        <div class="field-tip">经跳板机转发连接（ProxyJump）。跳板机自身也可以再配置跳板机，形成多级链路</div>
      </el-form-item>

      <el-form-item label="Agent 转发" v-if="form.type === 'ssh'">
        <el-switch v-model="form.forwardAgent" active-text="转发本地 SSH Agent" />
        <div class="field-tip">开启后，服务器内可复用本机 SSH Agent 的密钥（需本机运行 ssh-agent）</div>
      </el-form-item>

      <el-form-item label="默认目录">
        <el-input v-model="form.defaultDir" placeholder="连接后进入的远程目录（可选）" />
      </el-form-item>

      <template v-if="form.type === 'ftp'">
        <el-form-item label="FTPS">
          <el-switch v-model="form.tls" active-text="显式 TLS" />
        </el-form-item>
        <el-form-item label="跳过校验" v-if="form.tls">
          <el-switch v-model="form.insecure" active-text="跳过证书校验" />
        </el-form-item>
      </template>
    </el-form>

    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <template v-if="mode === 'connect'">
        <el-button type="primary" :loading="saving" @click="submitConnect(false)">连接</el-button>
        <el-button type="success" :loading="saving" @click="submitConnect(true)">保存并连接</el-button>
      </template>
      <template v-else>
        <el-button :loading="testing" :disabled="saving" @click="testConn">测试连接</el-button>
        <el-button type="primary" :loading="saving" :disabled="testing" @click="submitSave">保存</el-button>
      </template>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { makeConnectOptions, makeSavedConnection, LocalService, ConnService, SSHKeyService } from '../utils/wails'
import type { ConnectOptions, SavedConnection, SSHKey } from '../utils/wails'

type ConnType = 'ssh' | 'ftp'

const props = defineProps<{
  modelValue: boolean
  mode: 'create' | 'edit' | 'connect'
  connType: ConnType
  connection?: SavedConnection | null
  groups?: string[]
  // 可供选择的跳板机（已保存的 SSH 连接）
  connections?: SavedConnection[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'connect', opts: ConnectOptions, save: boolean): void
  (e: 'saved', conn: SavedConnection): void
}>()

// 跳板机候选：排除 FTP 连接和正在编辑的连接自身（避免自引用）
const jumpOptions = computed(() =>
  (props.connections ?? []).filter((c) => c.type !== 'ftp' && c.id !== props.connection?.id),
)

const formRef = ref<FormInstance>()
const saving = ref(false)
const testing = ref(false)
// 私钥文件路径只是读取入口，保存的仍是文件内容
const keyPath = ref('')
const loadingKey = ref(false)
// 认证密钥来源：引用密钥库 or 手动粘贴内容
const keySource = ref<'store' | 'manual'>('manual')
const managedKeys = ref<SSHKey[]>([])
const loadingKeys = ref(false)

const form = reactive({
  name: '',
  group: '',
  type: props.connType,
  host: '',
  port: props.connType === 'ftp' ? 21 : 22,
  username: 'root',
  password: '',
  useKey: false,
  privateKey: '',
  passphrase: '',
  keyId: 0,
  forwardAgent: false,
  jumpId: 0,
  defaultDir: '',
  tls: false,
  insecure: false,
})

const dialogTitle = computed(() => {
  if (props.mode === 'create') return '新建连接'
  if (props.mode === 'edit') return '编辑连接'
  return props.connType === 'ftp' ? '连接 FTP 服务器' : '新建 SSH 会话'
})

const rules = computed<FormRules>(() => ({
  name: [{ required: true, message: '请输入连接名称', trigger: 'blur' }],
  host: [{ required: true, message: '请输入主机地址', trigger: 'blur' }],
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  privateKey:
    form.useKey && keySource.value === 'manual'
      ? [{ required: true, message: '请粘贴私钥内容或选择私钥文件', trigger: 'blur' }]
      : [],
}))

async function loadManagedKeys() {
  if (loadingKeys.value) return
  loadingKeys.value = true
  try {
    managedKeys.value = (await SSHKeyService.List()) ?? []
  } catch (e: any) {
    ElMessage.error(`读取密钥库失败：${e?.message || e}`)
  } finally {
    loadingKeys.value = false
  }
}

async function loadKeyFromFile(path: string) {
  const p = path.trim()
  if (!p) return
  if (/\.pub$/i.test(p)) {
    ElMessage.error('这是公钥文件（.pub），无法用于登录；请选择不带 .pub 后缀的私钥文件')
    return
  }
  loadingKey.value = true
  try {
    const text = await LocalService.ReadTextFile(p)
    if (!text || !text.trim()) {
      ElMessage.error('该文件内容为空，请确认选择的是私钥文件')
      return
    }
    form.privateKey = text
    await formRef.value?.validateField('privateKey').catch(() => undefined)
    ElMessage.success(`已读取私钥：${p.split(/[\\/]/).pop()}`)
  } catch (e: any) {
    ElMessage.error(`读取私钥失败：${e?.message || e}`)
  } finally {
    loadingKey.value = false
  }
}

function loadKeyPath() {
  void loadKeyFromFile(keyPath.value)
}

async function pickKeyFile() {
  try {
    const p = await LocalService.PickOpenFile('选择私钥文件')
    if (!p) return
    keyPath.value = p
    await loadKeyFromFile(p)
  } catch (e: any) {
    ElMessage.error(`选择文件失败：${e?.message || e}`)
  }
}

watch(
  () => props.modelValue,
  (v) => {
    if (!v) return
    keyPath.value = ''
    loadingKey.value = false
    if (props.mode === 'edit' && props.connection) {
      Object.assign(form, {
        name: props.connection.name,
        group: props.connection.group,
        type: props.connection.type,
        host: props.connection.host,
        port: props.connection.port,
        username: props.connection.username,
        password: props.connection.password,
        useKey: props.connection.useKey,
        privateKey: props.connection.privateKey,
        passphrase: props.connection.passphrase,
        keyId: props.connection.keyId ?? 0,
        forwardAgent: props.connection.forwardAgent,
        jumpId: props.connection.jumpId ?? 0,
        defaultDir: props.connection.defaultDir,
        tls: props.connection.tls,
        insecure: false,
      })
      keySource.value = (props.connection.keyId ?? 0) > 0 ? 'store' : 'manual'
    } else {
      Object.assign(form, {
        name: '',
        group: '',
        type: props.connType,
        host: '',
        port: props.connType === 'ftp' ? 21 : 22,
        username: 'root',
        password: '',
        useKey: false,
        privateKey: '',
        passphrase: '',
        keyId: 0,
        forwardAgent: false,
        jumpId: 0,
        defaultDir: '',
        tls: false,
        insecure: false,
      })
      keySource.value = 'manual'
    }
    if (form.type === 'ssh' && !managedKeys.value.length) void loadManagedKeys()
    formRef.value?.clearValidate()
  },
)

// 认证字段：引用密钥库时只带 keyId，手动模式才带私钥内容
function keyFields() {
  if (form.type !== 'ssh' || !form.useKey) return { privateKey: '', passphrase: '', keyId: 0 }
  if (keySource.value === 'store') return { privateKey: '', passphrase: '', keyId: form.keyId || 0 }
  return { privateKey: form.privateKey, passphrase: form.passphrase, keyId: 0 }
}

function toConnectOptions(): ConnectOptions {
  return makeConnectOptions({
    host: form.host.trim(),
    port: form.port,
    username: form.username.trim(),
    password: form.password,
    useKey: form.useKey,
    ...keyFields(),
    forwardAgent: form.type === 'ssh' ? form.forwardAgent : false,
    jumpId: form.type === 'ssh' ? form.jumpId || 0 : 0,
    defaultDir: form.defaultDir.trim(),
    tls: form.tls,
    insecure: form.insecure,
  })
}

function toSavedConnection(): SavedConnection {
  return makeSavedConnection({
    id: props.connection?.id ?? 0,
    name: form.name.trim(),
    group: form.group.trim(),
    type: form.type,
    host: form.host.trim(),
    port: form.port,
    username: form.username.trim(),
    password: form.password,
    useKey: form.useKey,
    ...keyFields(),
    forwardAgent: form.type === 'ssh' ? form.forwardAgent : false,
    jumpId: form.type === 'ssh' ? form.jumpId || 0 : 0,
    defaultDir: form.defaultDir.trim(),
    tls: form.tls,
    // 编辑时保留原时间戳；新建时后端自动生成
    createdAt: props.connection?.createdAt,
    updatedAt: props.connection?.updatedAt,
  })
}

async function validate(): Promise<boolean> {
  if (form.type === 'ssh' && form.useKey && keySource.value === 'store' && !form.keyId) {
    ElMessage.warning('请选择密钥库中的密钥')
    return false
  }
  const ok = await formRef.value?.validate().catch(() => false)
  return ok === true
}

async function submitConnect(save: boolean) {
  if (!(await validate())) return
  saving.value = true
  try {
    emit('connect', toConnectOptions(), save)
  } finally {
    saving.value = false
  }
}

async function submitSave() {
  if (!(await validate())) return
  saving.value = true
  try {
    emit('saved', toSavedConnection())
  } finally {
    saving.value = false
  }
}

// 用当前表单（未保存）的值做一次真实登录测试；不要求填写名称
async function testConn() {
  if (!form.host.trim()) {
    ElMessage.warning('请输入主机地址')
    return
  }
  if (!form.username.trim()) {
    ElMessage.warning('请输入用户名')
    return
  }
  if (form.type === 'ssh' && form.useKey) {
    if (keySource.value === 'store' && !form.keyId) {
      ElMessage.warning('请选择密钥库中的密钥')
      return
    }
    if (keySource.value === 'manual' && !form.privateKey.trim()) {
      ElMessage.warning('请粘贴私钥内容或选择私钥文件')
      return
    }
  }
  if (form.type === 'ssh' && !form.useKey && !form.password) {
    ElMessage.warning('请输入密码，或切换为私钥认证')
    return
  }
  testing.value = true
  const started = Date.now()
  try {
    await ConnService.TestConnection(form.type, toConnectOptions())
    ElMessage.success(`连接成功（${Date.now() - started} ms）`)
  } catch (e: any) {
    ElMessage({ type: 'error', message: `连接失败：${e?.message || e}`, duration: 8000, showClose: true })
  } finally {
    testing.value = false
  }
}
</script>

<style scoped>
.row {
  display: flex;
  gap: 12px;
}
.row-host {
  flex: 1;
}
.row-port {
  width: 180px;
}
.row-port :deep(.el-input-number) {
  width: 100%;
}
.field-tip {
  width: 100%;
  font-size: 11.5px;
  color: var(--text-secondary);
  line-height: 1.4;
  margin-top: 4px;
}
</style>