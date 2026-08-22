<template>
  <div class="tv-view">
    <div class="tv-toolbar">
      <el-button size="small" type="primary" plain :disabled="!sessionId" @click="openLocal">
        本地转发
      </el-button>
      <el-button size="small" type="warning" plain :disabled="!sessionId" @click="openRemote">
        远程转发
      </el-button>
      <el-button size="small" type="success" plain :disabled="!sessionId" @click="openSocks">
        SOCKS5 代理
      </el-button>
      <el-button size="small" :loading="loading" @click="load">
        <el-icon style="margin-right: 2px"><Refresh /></el-icon>
      </el-button>
    </div>

    <div v-if="!sessionId" class="tv-hint">请先连接 SSH 会话，转发 / 代理将复用当前会话的连接</div>

    <div class="tv-scroll">
      <template v-if="tunnels.length">
        <div v-for="t in tunnels" :key="t.id" class="tv-item">
          <div class="tv-item-head">
            <el-tag :type="kindTag(t.kind)" size="small">{{ kindLabel(t.kind) }}</el-tag>
            <el-tag v-if="t.status === 'running'" size="small" type="success" effect="dark">运行中</el-tag>
            <el-tag v-else-if="t.status === 'error'" size="small" type="danger">错误</el-tag>
            <el-tag v-else size="small" type="info">{{ t.status }}</el-tag>
            <span class="tv-actions">
              <el-button size="small" text type="primary" @click="copy(t.bindAddr)">复制地址</el-button>
              <el-button size="small" text type="danger" @click="close(t)">关闭</el-button>
            </span>
          </div>

          <div class="tv-rows">
            <div class="tv-row">
              <span class="tv-label">监听</span>
              <span class="tv-value mono">{{ t.bindAddr }}</span>
            </div>
            <div class="tv-row">
              <span class="tv-label">{{ t.kind === 'remote' ? '本机目标' : '目标' }}</span>
              <span class="tv-value mono">{{ t.kind === 'socks' ? '动态（任意目标）' : t.target || '—' }}</span>
            </div>
            <div v-if="t.error" class="tv-row">
              <span class="tv-label">错误</span>
              <span class="tv-value tv-err">{{ t.error }}</span>
            </div>
          </div>
        </div>
      </template>

      <el-empty v-else-if="!loading" description="暂无转发 / 代理，点击上方按钮创建" :image-size="60" />
    </div>

    <div class="tv-foot">
      <span class="tv-info">
        {{ kindHint }}
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { Clipboard } from '@wailsio/runtime'
import { TerminalService } from '../utils/wails'
import type { Tunnel } from '../utils/wails'
import { showInputDialog, showConfirmDialog } from '../utils/dialog'

const props = defineProps<{
  sessionId: string
  active?: boolean
}>()

const tunnels = ref<Tunnel[]>([])
const loading = ref(false)
let timer: ReturnType<typeof setInterval> | null = null

const kindHint = '本地转发：本机端口 → 远端目标；远程转发：远端端口 → 本机目标；SOCKS5：本机动态代理，流量经服务器出口（可设用户名/密码，监听 0.0.0.0 供局域网使用）'

function kindLabel(kind: string): string {
  if (kind === 'local') return '本地转发'
  if (kind === 'remote') return '远程转发'
  if (kind === 'socks') return 'SOCKS5'
  return kind
}

function kindTag(kind: string): 'primary' | 'warning' | 'success' | 'info' | 'danger' {
  if (kind === 'local') return 'primary'
  if (kind === 'remote') return 'warning'
  if (kind === 'socks') return 'success'
  return 'info'
}

async function load() {
  if (!props.sessionId) {
    tunnels.value = []
    return
  }
  loading.value = true
  try {
    tunnels.value = (await TerminalService.Tunnels(props.sessionId)) ?? []
  } catch {
    tunnels.value = []
  } finally {
    loading.value = false
  }
}

async function openLocal() {
  const v = await showInputDialog('本地端口转发', [
    { key: 'bind', label: '本地监听地址', placeholder: '留空 = 127.0.0.1 随机端口；或 8080 / 127.0.0.1:8080', optional: true },
    { key: 'target', label: '远程目标 (host:port)', placeholder: '如 192.168.1.5:80 或 localhost:3306' },
  ])
  if (!v) return
  await create('local', v.bind, v.target)
}

async function openRemote() {
  const v = await showInputDialog('远程端口转发', [
    { key: 'bind', label: '远程监听地址', placeholder: '留空 = 127.0.0.1 随机端口；如 0.0.0.0:8080', optional: true },
    { key: 'target', label: '本机目标 (host:port)', placeholder: '如 127.0.0.1:3000' },
  ])
  if (!v) return
  await create('remote', v.bind, v.target)
}

async function openSocks() {
  const v = await showInputDialog('SOCKS5 动态代理', [
    { key: 'bind', label: '本地监听地址', placeholder: '留空 = 127.0.0.1 随机端口；局域网用 0.0.0.0:1080', optional: true },
    { key: 'user', label: '用户名（可选，留空=无需认证）', placeholder: '可选', optional: true },
    { key: 'pass', label: '密码（可选）', placeholder: '可选', optional: true, type: 'password' },
  ])
  if (!v) return
  await create('socks', v.bind, '', v.user, v.pass)
}

async function create(kind: string, bind: string, target: string, user = '', pass = '') {
  try {
    const t = await TerminalService.OpenTunnel(props.sessionId, kind, bind, target, user, pass)
    ElMessage.success(`已建立${kindLabel(kind)}：${t.bindAddr}`)
    await load()
  } catch (e: any) {
    ElMessage.error(`建立失败：${e?.message || e}`)
  }
}

async function close(t: Tunnel) {
  const ok = await showConfirmDialog('关闭转发', `确定关闭「${kindLabel(t.kind)} ${t.bindAddr}」？`, true, '关闭')
  if (!ok) return
  try {
    await TerminalService.CloseTunnel(t.id)
    await load()
  } catch (e: any) {
    ElMessage.error(`关闭失败：${e?.message || e}`)
  }
}

async function copy(text: string) {
  try {
    await Clipboard.SetText(text)
    ElMessage.success(`已复制 ${text}`)
  } catch (e: any) {
    ElMessage.error(`复制失败：${e?.message || e}`)
  }
}

function startTimer() {
  stopTimer()
  if (!props.active || !props.sessionId) return
  load()
  // 轻量轮询：仅读本地内存里的隧道表，会话断开后自动清空列表
  timer = setInterval(load, 3000)
}

function stopTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

watch(() => props.active, startTimer)
watch(
  () => props.sessionId,
  () => {
    tunnels.value = []
    startTimer()
  },
)

onBeforeUnmount(stopTimer)
</script>

<style scoped>
.tv-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.tv-toolbar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 10px 6px;
  flex-shrink: 0;
  flex-wrap: wrap;
}

.tv-hint {
  font-size: 11.5px;
  color: var(--text-secondary);
  padding: 0 10px 6px;
}

.tv-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 4px 10px 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tv-item {
  background: var(--hover-bg);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.tv-item-head {
  display: flex;
  align-items: center;
  gap: 6px;
}

.tv-actions {
  margin-left: auto;
  display: flex;
  gap: 2px;
}

.tv-rows {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.tv-row {
  display: flex;
  gap: 8px;
  align-items: baseline;
}

.tv-label {
  font-size: 11.5px;
  color: var(--text-secondary);
  flex-shrink: 0;
  width: 52px;
}

.tv-value {
  font-size: 12px;
  color: var(--text-primary);
  word-break: break-all;
}

.tv-err {
  color: #f56c6c;
}

.tv-foot {
  flex-shrink: 0;
  padding: 6px 10px;
  border-top: 1px solid var(--border-color);
}

.tv-info {
  font-size: 11px;
  color: var(--text-muted);
  line-height: 1.5;
}

.mono {
  font-family: var(--term-font);
}
</style>
