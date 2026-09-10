<template>
  <div class="sf-view">
    <div class="sf-toolbar">
      <el-button size="small" type="primary" plain :disabled="!canCreate" :loading="creating" @click="create">
        新建系统转发
      </el-button>
      <el-button size="small" :loading="loading" @click="load">
        <el-icon style="margin-right: 2px"><Refresh /></el-icon>
      </el-button>
    </div>

    <div v-if="!sessionId" class="sf-hint">请先连接 SSH 会话，系统转发通过该会话在服务器上下发防火墙规则</div>
    <template v-else-if="status">
      <!-- 不可用时只说明原因；可用时若有告警（例如 ufw 与 firewalld 冲突）也要提示 -->
      <div v-if="!status.available" class="sf-warn">
        <el-tag size="small" type="warning">{{ status.backendName }}</el-tag>
        <span>{{ status.message || '系统转发当前不可用' }}</span>
      </div>
      <template v-else>
        <div v-if="status.message" class="sf-warn">
          <el-tag size="small" type="danger">注意</el-tag>
          <span>{{ status.message }}</span>
        </div>
        <div class="sf-meta">
          <el-tag size="small" type="info">{{ status.backendName }}</el-tag>
          <el-tag size="small" :type="status.privilege === 'none' ? 'danger' : 'success'">
            {{ status.privilege === 'root' ? 'root' : status.privilege === 'sudo' ? '免密 sudo' : '无权限' }}
          </el-tag>
          <el-tag size="small" :type="status.ipForward ? 'success' : 'warning'">
            {{ status.ipForward ? '已开启 IP 转发' : 'IP 转发关闭' }}
          </el-tag>
          <el-tag v-if="status.zone" size="small" type="info">区域 {{ status.zone }}</el-tag>
          <el-tag size="small" type="info">共 {{ status.total }} 条</el-tag>
        </div>
      </template>
    </template>

    <div class="sf-scroll">
      <template v-if="rules.length">
        <div v-for="r in rules" :key="r.id" class="sf-item">
          <div class="sf-item-head">
            <el-tag size="small" type="primary">{{ r.proto.toUpperCase() }}</el-tag>
            <el-tag v-if="r.managed" size="small" type="success" effect="dark">本应用创建</el-tag>
            <el-tag v-else size="small" type="warning">系统已有</el-tag>
            <span class="sf-actions">
              <el-button size="small" text type="primary" @click="copy(r)">复制</el-button>
              <el-button size="small" text type="danger" @click="remove(r)">删除</el-button>
            </span>
          </div>

          <div class="sf-rows">
            <div class="sf-row">
              <span class="sf-label">转发</span>
              <span class="sf-value mono">
                {{ r.srcPort }} → {{ r.destIp || '本机' }}:{{ r.destPort }}
              </span>
            </div>
            <div v-if="r.note" class="sf-row">
              <span class="sf-label">备注</span>
              <span class="sf-value">{{ r.note }}</span>
            </div>
            <div v-if="r.createdAt" class="sf-row">
              <span class="sf-label">创建于</span>
              <span class="sf-value">{{ r.createdAt }}</span>
            </div>
            <div v-if="r.raw && !r.managed" class="sf-row">
              <span class="sf-label">规则</span>
              <span class="sf-value mono sf-raw">{{ r.raw }}</span>
            </div>
          </div>
        </div>
      </template>

      <el-empty v-else-if="!loading && sessionId && status?.available" description="暂无系统转发规则" :image-size="60" />
    </div>

    <div class="sf-foot">
      <span class="sf-info">{{ footHint }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { Clipboard } from '@wailsio/runtime'
import { TerminalService } from '../utils/wails'
import type { SystemForward, SystemForwardStatus } from '../utils/wails'
import { showInputDialog, showConfirmDialog } from '../utils/dialog'

const props = defineProps<{
  sessionId: string
  active?: boolean
}>()

const status = ref<SystemForwardStatus | null>(null)
const loading = ref(false)
const creating = ref(false)

const rules = computed<SystemForward[]>(() => status.value?.rules ?? [])
const canCreate = computed(() => !!props.sessionId && !!status.value?.available)

const footHint = computed(() => {
  const s = status.value
  if (!s) return '系统转发把 DNAT / MASQUERADE 规则写进服务器防火墙，断开 SSH 后依然生效'
  const base = '系统转发 = 服务器防火墙 DNAT + MASQUERADE（不需要本机保持连接）'
  return s.persist ? `${base}；持久化：${s.persist}` : base
})

async function load() {
  if (!props.sessionId) {
    status.value = null
    return
  }
  loading.value = true
  try {
    status.value = (await TerminalService.SystemForwardStatus(props.sessionId)) ?? null
  } catch (e: any) {
    status.value = null
    ElMessage.error(`读取系统转发失败：${e?.message || e}`)
  } finally {
    loading.value = false
  }
}

async function create() {
  const s = status.value
  const fields = [
    {
      key: 'proto',
      label: '协议',
      type: 'select' as const,
      initial: 'tcp',
      options: [
        { label: 'TCP', value: 'tcp' },
        { label: 'UDP', value: 'udp' },
      ],
    },
    { key: 'srcPort', label: '服务器对外端口', placeholder: '如 8080（外部访问服务器这个端口）' },
    { key: 'destIp', label: '转发目标地址 (IPv4)', placeholder: '如 10.0.0.5；本机服务请填服务器内网 IP' },
    { key: 'destPort', label: '目标端口', placeholder: '如 80' },
    { key: 'note', label: '备注（可选）', placeholder: '如 内部测试环境', optional: true },
  ]
  if (s?.backend === 'firewalld') {
    fields.push({
      key: 'zone',
      label: 'firewalld 区域（可选，留空=默认区域）',
      placeholder: s.zone || 'public',
      optional: true,
    } as any)
  }
  const v = await showInputDialog('新建系统转发', fields)
  if (!v) return

  const srcPort = Number(v.srcPort)
  const destPort = Number(v.destPort)
  if (!Number.isInteger(srcPort) || srcPort < 1 || srcPort > 65535) {
    ElMessage.warning('服务器对外端口需为 1-65535 的整数')
    return
  }
  if (!Number.isInteger(destPort) || destPort < 1 || destPort > 65535) {
    ElMessage.warning('目标端口需为 1-65535 的整数')
    return
  }

  creating.value = true
  try {
    status.value =
      (await TerminalService.AddSystemForward(props.sessionId, {
        proto: v.proto || 'tcp',
        srcPort,
        destIp: (v.destIp || '').trim(),
        destPort,
        note: (v.note || '').trim(),
        zone: (v.zone || '').trim(),
      })) ?? null
    ElMessage.success(`已创建系统转发：${v.proto} ${srcPort} → ${v.destIp}:${destPort}`)
  } catch (e: any) {
    ElMessage.error(`创建失败：${e?.message || e}`)
  } finally {
    creating.value = false
  }
}

async function remove(r: SystemForward) {
  const tip = r.managed
    ? `确定删除「${r.proto.toUpperCase()} ${r.srcPort} → ${r.destIp}:${r.destPort}」及其配套规则？`
    : `「${r.proto.toUpperCase()} ${r.srcPort} → ${r.destIp}:${r.destPort}」不是本应用创建的规则（可能是 Docker 或其它软件），删除可能影响现有服务，确定继续？`
  const ok = await showConfirmDialog('删除系统转发', tip, true, '删除')
  if (!ok) return
  try {
    status.value = (await TerminalService.RemoveSystemForward(props.sessionId, r.id)) ?? null
    ElMessage.success('已删除')
  } catch (e: any) {
    ElMessage.error(`删除失败：${e?.message || e}`)
  }
}

async function copy(r: SystemForward) {
  try {
    await Clipboard.SetText(`${r.destIp}:${r.destPort}`)
    ElMessage.success(`已复制 ${r.destIp}:${r.destPort}`)
  } catch (e: any) {
    ElMessage.error(`复制失败：${e?.message || e}`)
  }
}

// 只有面板可见时才探测（探测要开一条远端命令通道，不适合后台轮询）
watch(
  () => [props.active, props.sessionId],
  () => {
    if (props.active && props.sessionId) load()
    if (!props.sessionId) status.value = null
  },
  { immediate: true },
)
</script>

<style scoped>
.sf-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.sf-toolbar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 10px 6px;
  flex-shrink: 0;
}

.sf-hint,
.sf-warn,
.sf-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  padding: 0 10px 8px;
  font-size: 11.5px;
  color: var(--text-secondary);
  flex-shrink: 0;
}

.sf-warn {
  color: #e6a23c;
}

.sf-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 4px 10px 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sf-item {
  background: var(--hover-bg);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.sf-item-head {
  display: flex;
  align-items: center;
  gap: 6px;
}

.sf-actions {
  margin-left: auto;
  display: flex;
  gap: 2px;
}

.sf-rows {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.sf-row {
  display: flex;
  gap: 8px;
  align-items: baseline;
}

.sf-label {
  font-size: 11.5px;
  color: var(--text-secondary);
  flex-shrink: 0;
  width: 52px;
}

.sf-value {
  font-size: 12px;
  color: var(--text-primary);
  word-break: break-all;
}

.sf-raw {
  color: var(--text-muted);
  font-size: 11px;
}

.sf-foot {
  flex-shrink: 0;
  padding: 6px 10px;
  border-top: 1px solid var(--border-color);
}

.sf-info {
  font-size: 11px;
  color: var(--text-muted);
  line-height: 1.5;
}

.mono {
  font-family: var(--term-font);
}
</style>
