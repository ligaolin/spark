<template>
  <div class="nw-view">
    <div class="nw-toolbar">
      <el-button size="small" type="primary" plain :loading="loading" @click="refresh">
        <el-icon style="margin-right: 4px"><Refresh /></el-icon>
        刷新
      </el-button>
      <el-switch v-model="auto" size="small" active-text="自动" />
      <el-tag v-if="disconnected" size="small" type="danger">连接已断开</el-tag>
      <el-tag v-else-if="errorMsg" size="small" type="warning" class="nw-warn">{{ errorMsg }}</el-tag>
      <el-tag v-else-if="info?.error" size="small" type="warning" class="nw-warn">{{ info.error }}</el-tag>
      <el-tag v-else-if="info" size="small" type="success" effect="dark">已连接</el-tag>
    </div>

    <div class="nw-scroll">
      <template v-if="info">
        <!-- 网络接口 -->
        <div class="nw-block">
          <div class="nw-block-title">网络接口</div>
          <div v-if="info.interfaces?.length" class="nw-ifaces">
            <div v-for="iface in info.interfaces" :key="iface.name" class="nw-iface">
              <div class="nw-iface-head">
                <span class="nw-iface-name mono">{{ iface.name }}</span>
                <el-tag :type="stateTag(iface.state)" size="small">{{ iface.state }}</el-tag>
              </div>
              <div class="nw-iface-meta">
                <span v-if="iface.mac" class="mono">{{ iface.mac }}</span>
                <span v-if="iface.mtu">MTU {{ iface.mtu }}</span>
              </div>
              <div v-if="iface.addresses?.length" class="nw-iface-addrs mono">
                {{ iface.addresses.join(' · ') }}
              </div>
              <div class="nw-iface-traffic">
                <span class="nw-rate rx">↓ {{ formatRate(rates[iface.name]?.rx) }}</span>
                <span class="nw-rate tx">↑ {{ formatRate(rates[iface.name]?.tx) }}</span>
                <span class="nw-total">累计 ↓ {{ formatSize(iface.rxBytes) }} / ↑ {{ formatSize(iface.txBytes) }}</span>
              </div>
            </div>
          </div>
          <div v-else class="nw-empty">暂无接口数据</div>
        </div>

        <!-- 监听端口 -->
        <div class="nw-block">
          <div class="nw-block-title">监听端口</div>
          <el-table
            v-if="listeners.length"
            :data="listeners"
            size="small"
            max-height="260"
            empty-text="暂无监听端口"
          >
            <el-table-column label="协议" width="70">
              <template #default="{ row }">
                <span class="mono">{{ row.proto }}</span>
              </template>
            </el-table-column>
            <el-table-column label="本地地址" min-width="150">
              <template #default="{ row }">
                <span class="mono">{{ row.address }}</span>
              </template>
            </el-table-column>
            <el-table-column label="进程" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">
                <span v-if="row.process" class="mono">
                  {{ row.process }}<span v-if="row.pid" class="dim"> ({{ row.pid }})</span>
                </span>
                <span v-else class="dim">—</span>
              </template>
            </el-table-column>
          </el-table>
          <div v-else class="nw-empty">暂无监听端口</div>
        </div>

        <!-- 路由表 -->
        <div class="nw-block">
          <div class="nw-block-title">路由表</div>
          <el-table
            v-if="info.routes?.length"
            :data="info.routes"
            size="small"
            max-height="220"
            empty-text="暂无路由信息"
          >
            <el-table-column label="目标" min-width="140">
              <template #default="{ row }">
                <span class="mono">{{ routeDest(row.destination) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="网关" min-width="130">
              <template #default="{ row }">
                <span class="mono">{{ row.gateway || '—' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="接口" width="110">
              <template #default="{ row }">
                <span class="mono">{{ row.interface || '—' }}</span>
              </template>
            </el-table-column>
          </el-table>
          <div v-else class="nw-empty">暂无路由信息</div>
        </div>

        <!-- DNS -->
        <div class="nw-block">
          <div class="nw-block-title">DNS</div>
          <div v-if="info.dns?.length" class="nw-dns">
            <el-tag v-for="d in info.dns" :key="d" size="small" class="mono">{{ d }}</el-tag>
          </div>
          <div v-else class="nw-empty">暂无 DNS 配置</div>
        </div>
      </template>

      <el-empty v-else-if="!loading && !errorMsg" description="暂无数据，点击刷新获取" :image-size="60" />
    </div>

    <div class="nw-foot">
      <span class="nw-info">每 {{ settings.networkRefreshInterval }} 秒自动刷新基础信息，监听端口每 30 秒刷新一次</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { TerminalService } from '../utils/wails'
import type { NetworkInfo, NetListener } from '../utils/wails'
import { useSettingsStore } from '../stores/settings'
import { formatSize } from '../types'

const props = defineProps<{
  sessionId: string
  // 面板是否处于激活状态（用于控制自动刷新）
  active?: boolean
}>()

const settings = useSettingsStore()

const info = ref<NetworkInfo | null>(null)
const listeners = ref<NetListener[]>([])
const loading = ref(false)
const auto = ref(true)
const errorMsg = ref('')
const disconnected = ref(false)

// 各接口 RX/TX 速率（bytes/s），由相邻两次 NetworkInfo 采样的计数器差值计算
const rates = ref<Record<string, { rx: number; tx: number }>>({})
let prev: Record<string, { rx: number; tx: number }> = {}
let prevAt = 0

let timer: ReturnType<typeof setInterval> | null = null
let inflight = false
let listenersInflight = false

// 监听端口变化慢，单独以更长间隔采集
let lastListenersAt = 0
const LISTENERS_REFRESH_MS = 30_000

function stateTag(state: string): 'success' | 'danger' | 'info' {
  if (state === 'up') return 'success'
  if (state === 'down') return 'danger'
  return 'info'
}

function routeDest(d: string): string {
  if (!d) return '—'
  return d === 'default' ? '默认 (0.0.0.0/0)' : d
}

function formatRate(bps: number | undefined): string {
  if (bps === undefined) return '—'
  if (bps <= 0) return '0 B/s'
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  const i = Math.min(units.length - 1, Math.floor(Math.log(bps) / Math.log(1024)))
  const v = bps / Math.pow(1024, i)
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

async function loadListeners() {
  if (!props.sessionId || disconnected.value || listenersInflight) return
  const now = Date.now()
  if (lastListenersAt && now - lastListenersAt < LISTENERS_REFRESH_MS) return
  listenersInflight = true
  try {
    listeners.value = (await TerminalService.NetworkListeners(props.sessionId)) ?? []
    lastListenersAt = now
  } catch {
    // 静默保留旧数据
  } finally {
    listenersInflight = false
  }
}

async function refresh() {
  if (!props.sessionId || inflight) return
  inflight = true
  loading.value = true
  try {
    const data = await TerminalService.NetworkInfo(props.sessionId)
    if (!data) {
      info.value = null
      return
    }
    disconnected.value = false
    errorMsg.value = ''

    // 用相邻两次采样的字节差计算实时速率
    const now = Date.now()
    const next: Record<string, { rx: number; tx: number }> = {}
    for (const iface of data.interfaces ?? []) {
      const p = prev[iface.name]
      if (p && prevAt > 0 && now > prevAt) {
        const dt = (now - prevAt) / 1000
        next[iface.name] = {
          rx: Math.max(0, (iface.rxBytes - p.rx) / dt),
          tx: Math.max(0, (iface.txBytes - p.tx) / dt),
        }
      }
      prev[iface.name] = { rx: iface.rxBytes, tx: iface.txBytes }
    }
    prevAt = now
    rates.value = next
    info.value = data
  } catch (e: any) {
    const msg = e?.message || String(e)
    if (/会话.*(不存在|已关闭)|不存在|已关闭/.test(msg)) {
      disconnected.value = true
      errorMsg.value = ''
      info.value = null
      stopTimer()
    } else if (!info.value) {
      // 首次就失败：内联提示，不弹 toast 刷屏
      errorMsg.value = msg
    }
    // 已有数据时静默保留旧数据
  } finally {
    loading.value = false
    inflight = false
  }
  await loadListeners()
}

function stopTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

function startTimer() {
  stopTimer()
  if (!props.active || !props.sessionId || disconnected.value) return
  refresh()
  if (auto.value) {
    timer = setInterval(refresh, settings.networkRefreshInterval * 1000)
  }
}

watch(() => props.active, (active) => {
  if (!active) {
    // 面板隐藏时重置速率基线：下次激活先采样一次作为基准，
    // 避免用跨长间隔的差值算出忽高忽低的瞬时速率。
    prev = {}
    prevAt = 0
    rates.value = {}
  }
  startTimer()
})
watch(
  () => props.sessionId,
  (v) => {
    prev = {}
    prevAt = 0
    rates.value = {}
    info.value = null
    listeners.value = []
    lastListenersAt = 0
    errorMsg.value = ''
    disconnected.value = false
    if (v) startTimer()
  },
)
watch(auto, startTimer)
watch(() => settings.networkRefreshInterval, startTimer)

onBeforeUnmount(stopTimer)
</script>

<style scoped>
.nw-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.nw-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 10px 6px;
  flex-shrink: 0;
}

.nw-warn {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.nw-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 4px 10px 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.nw-block {
  background: var(--hover-bg);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.nw-block-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--accent);
}

.nw-ifaces {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.nw-iface {
  background: var(--panel-bg);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.nw-iface-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.nw-iface-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}

.nw-iface-meta {
  display: flex;
  gap: 12px;
  font-size: 11.5px;
  color: var(--text-secondary);
}

.nw-iface-addrs {
  font-size: 11.5px;
  color: var(--text-primary);
  word-break: break-all;
}

.nw-iface-traffic {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 2px;
}

.nw-rate {
  font-size: 12px;
  font-weight: 600;
}

.nw-rate.rx {
  color: #4f8cff;
}

.nw-rate.tx {
  color: #34c759;
}

.nw-total {
  font-size: 11px;
  color: var(--text-muted);
}

.nw-dns {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.nw-empty {
  font-size: 12px;
  color: var(--text-muted);
  padding: 4px 0;
}

.nw-foot {
  flex-shrink: 0;
  padding: 6px 10px;
  border-top: 1px solid var(--border-color);
}

.nw-info {
  font-size: 11.5px;
  color: var(--text-secondary);
}

.mono {
  font-family: var(--term-font);
}

.dim {
  color: var(--text-secondary);
}
</style>
