<template>
  <div class="cc-view">
    <!-- 临时命令：直接发送到左侧终端 -->
    <div class="cc-adhoc">
      <el-input
        v-model="adhoc"
        size="small"
        placeholder="输入命令，回车发送到终端"
        :disabled="!sessionId"
        @keyup.enter="sendAdhoc"
      />
      <el-button size="small" type="primary" :disabled="!sessionId" @click="sendAdhoc">
        发送
      </el-button>
    </div>
    <div v-if="!sessionId" class="cc-hint">请先连接 SSH 会话，命令将直接发送到左侧终端执行</div>

    <!-- 保存的命令列表（数据库） -->
    <div class="cc-list">
      <div v-for="cmd in store.items" :key="cmd.id" class="cc-item">
        <div class="cc-item-head">
          <span class="cc-name" :title="cmd.name">{{ cmd.name }}</span>
          <el-button
            size="small"
            text
            type="primary"
            :disabled="!sessionId"
            @click="send(cmd.command, cmd.name)"
          >
            发送
          </el-button>
          <el-button size="small" text @click="edit(cmd)">编辑</el-button>
          <el-button size="small" text type="danger" @click="remove(cmd)">删除</el-button>
        </div>
        <div class="cc-cmd mono" :title="cmd.command">{{ cmd.command }}</div>
      </div>
      <el-button size="small" class="cc-add" @click="add">＋ 新增命令</el-button>
    </div>

    <!-- 发送记录 -->
    <div class="cc-output">
      <div class="cc-output-head">
        <span>发送记录</span>
        <el-button v-if="sent.length" size="small" text @click="sent = []">清空</el-button>
      </div>
      <div v-if="sent.length" class="cc-log">
        <div v-for="(s, i) in sent" :key="i" class="cc-log-item">
          <span class="cc-log-time">{{ s.time }}</span>
          <span class="cc-log-cmd mono">{{ s.command }}</span>
        </div>
      </div>
      <div v-else class="cc-empty">命令将直接发送到左侧终端执行</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { TerminalService } from '../utils/wails'
import type { CustomCommand } from '../utils/wails'
import { showInputDialog, showConfirmDialog } from '../utils/dialog'
import { useCustomCommandsStore } from '../stores/customCommands'

const props = defineProps<{ sessionId: string }>()

const store = useCustomCommandsStore()
const adhoc = ref('')
const sent = ref<{ time: string; command: string }[]>([])

onMounted(() => store.load())

// 把命令里的换行统一成终端的回车（\r），多行命令按"逐行输入 + 回车"发送
function toTerminalInput(command: string): string {
  const lines = command.replace(/\r\n/g, '\n').replace(/\r/g, '\n').split('\n')
  return lines.join('\r') + '\r'
}

// 把命令直接写入终端（模拟输入 + 回车），在左侧终端里执行
async function sendToTerminal(command: string) {
  if (!props.sessionId) {
    ElMessage.warning('请先连接 SSH 会话')
    return false
  }
  if (!command.trim()) return false
  try {
    await TerminalService.Write(props.sessionId, toTerminalInput(command))
    const now = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    sent.value.unshift({
      time: `${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`,
      command,
    })
    if (sent.value.length > 50) sent.value.length = 50
    return true
  } catch (e: any) {
    ElMessage.error(`发送失败：${e?.message || e}`)
    return false
  }
}

async function sendAdhoc() {
  const ok = await sendToTerminal(adhoc.value)
  if (ok) adhoc.value = ''
}

async function send(command: string, _name: string) {
  await sendToTerminal(command)
}

// 新增命令：一次弹窗同时填名称 + 命令内容
async function add() {
  const values = await showInputDialog('新增命令', [
    { key: 'name', label: '命令名称', placeholder: '如：查看日志' },
    { key: 'command', label: '命令内容（支持多行）', placeholder: '如：tail -n 100 /var/log/syslog', type: 'textarea' },
  ])
  if (!values) return
  const name = values.name.trim()
  const command = values.command.trim()
  if (!name || !command) {
    ElMessage.warning('名称和命令不能为空')
    return
  }
  try {
    await store.add(name, command)
    ElMessage.success('已保存')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  }
}

// 编辑命令：一次弹窗改名称 + 命令内容
async function edit(cmd: CustomCommand) {
  const values = await showInputDialog('编辑命令', [
    { key: 'name', label: '命令名称', initial: cmd.name },
    { key: 'command', label: '命令内容（支持多行）', initial: cmd.command, type: 'textarea' },
  ])
  if (!values) return
  const name = values.name.trim()
  const command = values.command.trim()
  if (!name || !command) {
    ElMessage.warning('名称和命令不能为空')
    return
  }
  try {
    await store.update(cmd.id, name, command)
    ElMessage.success('已更新')
  } catch (e: any) {
    ElMessage.error(`更新失败：${e?.message || e}`)
  }
}

async function remove(cmd: CustomCommand) {
  const ok = await showConfirmDialog('删除命令', `确定删除命令「${cmd.name}」？`, true, '删除')
  if (!ok) return
  try {
    await store.remove(cmd.id)
  } catch (e: any) {
    ElMessage.error(`删除失败：${e?.message || e}`)
  }
}
</script>

<style scoped>
.cc-view {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 10px;
  height: 100%;
  overflow-y: auto;
}

.cc-adhoc {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

.cc-hint {
  font-size: 11.5px;
  color: var(--text-secondary);
}

.cc-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex-shrink: 0;
}

.cc-item {
  background: var(--hover-bg);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  padding: 6px 10px;
}

.cc-item-head {
  display: flex;
  align-items: center;
  gap: 2px;
}

.cc-name {
  flex: 1;
  font-size: 12.5px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cc-cmd {
  font-size: 11.5px;
  color: var(--text-secondary);
  margin-top: 3px;
  word-break: break-all;
  white-space: pre-wrap;
}

.cc-add {
  align-self: flex-start;
}

.cc-output {
  flex: 1;
  min-height: 120px;
  display: flex;
  flex-direction: column;
  background: var(--term-bg);
  border: 1px solid var(--border-color);
  border-radius: 6px;
  overflow: hidden;
}

.cc-output-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 10px;
  font-size: 12px;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}

.cc-log {
  flex: 1;
  overflow-y: auto;
  padding: 6px 10px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.cc-log-item {
  display: flex;
  gap: 8px;
  font-size: 11.5px;
}

.cc-log-time {
  color: var(--text-muted);
  flex-shrink: 0;
}

.cc-log-cmd {
  color: var(--text-primary);
  word-break: break-all;
  white-space: pre-wrap;
}

.cc-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  color: var(--text-muted);
}
</style>
