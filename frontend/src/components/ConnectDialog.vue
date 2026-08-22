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
        <el-form-item label="私钥" prop="privateKey">
          <el-input
            v-model="form.privateKey"
            type="textarea"
            :rows="5"
            placeholder="粘贴 PEM 格式私钥内容（RSA / EC / OPENSSH）"
          />
        </el-form-item>
        <el-form-item label="密钥口令">
          <el-input v-model="form.passphrase" type="password" show-password placeholder="加密私钥的口令（可选）" />
        </el-form-item>
      </template>

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
        <el-button type="primary" :loading="saving" @click="submitSave">保存</el-button>
      </template>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch, computed } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { makeConnectOptions, makeSavedConnection } from '../utils/wails'
import type { ConnectOptions, SavedConnection } from '../utils/wails'

type ConnType = 'ssh' | 'ftp'

const props = defineProps<{
  modelValue: boolean
  mode: 'create' | 'edit' | 'connect'
  connType: ConnType
  connection?: SavedConnection | null
  groups?: string[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'connect', opts: ConnectOptions, save: boolean): void
  (e: 'saved', conn: SavedConnection): void
}>()

const formRef = ref<FormInstance>()
const saving = ref(false)

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
  defaultDir: '',
  tls: false,
  insecure: false,
})

const dialogTitle = computed(() => {
  if (props.mode === 'create') return '新建连接'
  if (props.mode === 'edit') return '编辑连接'
  return props.connType === 'ftp' ? '连接 FTP 服务器' : '新建 SSH 会话'
})

const rules: FormRules = {
  name: [{ required: true, message: '请输入连接名称', trigger: 'blur' }],
  host: [{ required: true, message: '请输入主机地址', trigger: 'blur' }],
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  privateKey: [{ required: true, message: '请粘贴私钥内容', trigger: 'blur' }],
}

watch(
  () => props.modelValue,
  (v) => {
    if (!v) return
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
        defaultDir: props.connection.defaultDir,
        tls: props.connection.tls,
        insecure: false,
      })
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
        defaultDir: '',
        tls: false,
        insecure: false,
      })
    }
    formRef.value?.clearValidate()
  },
)

function toConnectOptions(): ConnectOptions {
  return makeConnectOptions({
    host: form.host.trim(),
    port: form.port,
    username: form.username.trim(),
    password: form.password,
    useKey: form.useKey,
    privateKey: form.privateKey,
    passphrase: form.passphrase,
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
    privateKey: form.privateKey,
    passphrase: form.passphrase,
    defaultDir: form.defaultDir.trim(),
    tls: form.tls,
    // 编辑时保留原时间戳；新建时后端自动生成
    createdAt: props.connection?.createdAt,
    updatedAt: props.connection?.updatedAt,
  })
}

async function validate(): Promise<boolean> {
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
</style>