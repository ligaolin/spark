<template>
  <div class="cfg-list">
    <div class="cfg-row">
      <div class="cfg-info">
        <div class="cfg-label">连接保活间隔（秒）</div>
        <div class="cfg-desc">SSH / SFTP 发 keepalive、FTP 发 NOOP 的间隔；填 0 表示关闭保活。修改后对之后建立的连接生效</div>
      </div>
      <div class="cfg-ctrl">
        <el-input-number v-model="keepaliveVal" :min="0" :max="300" :step="5" size="small" />
        <el-button size="small" type="primary" :loading="savingKeepalive" @click="saveKeepalive">
          保存
        </el-button>
      </div>
    </div>

    <div class="cfg-row">
      <div class="cfg-info">
        <div class="cfg-label">进程管理自动刷新间隔（秒）</div>
        <div class="cfg-desc">进程列表定时刷新的频率，设置后立即生效</div>
      </div>
      <div class="cfg-ctrl">
        <el-input-number v-model="processVal" :min="1" :max="300" :step="1" size="small" />
        <el-button size="small" type="primary" :loading="savingProcess" @click="saveProcess">
          保存
        </el-button>
      </div>
    </div>

    <div class="cfg-row">
      <div class="cfg-info">
        <div class="cfg-label">终端字号</div>
        <div class="cfg-desc">SSH 终端字体大小，保存后立即对已打开的终端生效</div>
      </div>
      <div class="cfg-ctrl">
        <el-input-number v-model="fontVal" :min="8" :max="32" :step="1" size="small" />
        <el-button size="small" type="primary" :loading="savingFont" @click="saveFont">
          保存
        </el-button>
      </div>
    </div>

    <div class="cfg-row">
      <div class="cfg-info">
        <div class="cfg-label">点击窗口关闭按钮时</div>
        <div class="cfg-desc">
          「缩小到托盘」会把窗口隐藏到任务栏右下角的托盘区，程序继续在后台运行（连接保持不断）；
          左键点击托盘图标可重新显示窗口，右键点击可选择退出。保存后立即生效
        </div>
      </div>
      <div class="cfg-ctrl">
        <el-select v-model="closeActionVal" size="small" style="width: 132px">
          <el-option label="缩小到托盘" value="minimize" />
          <el-option label="直接退出" value="exit" />
        </el-select>
        <el-button size="small" type="primary" :loading="savingCloseAction" @click="saveCloseAction">
          保存
        </el-button>
      </div>
    </div>

    <div class="cfg-note">更多配置项会陆续加到这里（配置存于本地数据库 settings 表）。</div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useSettingsStore } from '../stores/settings'

const settings = useSettingsStore()

const keepaliveVal = ref(20)
const processVal = ref(5)
const fontVal = ref(13)
const closeActionVal = ref<'minimize' | 'exit'>('minimize')
const savingKeepalive = ref(false)
const savingProcess = ref(false)
const savingFont = ref(false)
const savingCloseAction = ref(false)

onMounted(async () => {
  await settings.load()
  keepaliveVal.value = settings.keepaliveInterval
  processVal.value = settings.processRefreshInterval
  fontVal.value = settings.terminalFontSize
  closeActionVal.value = settings.windowCloseAction
})

async function saveKeepalive() {
  savingKeepalive.value = true
  try {
    await settings.set('keepalive.interval', String(keepaliveVal.value))
    ElMessage.success('已保存（对之后的连接生效）')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingKeepalive.value = false
  }
}

async function saveProcess() {
  savingProcess.value = true
  try {
    await settings.set('process.refresh', String(processVal.value))
    ElMessage.success('已保存')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingProcess.value = false
  }
}

async function saveFont() {
  savingFont.value = true
  try {
    await settings.set('terminal.fontSize', String(fontVal.value))
    ElMessage.success('已保存')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingFont.value = false
  }
}

async function saveCloseAction() {
  savingCloseAction.value = true
  try {
    await settings.set('window.closeAction', closeActionVal.value)
    ElMessage.success(
      closeActionVal.value === 'exit' ? '已保存：关闭按钮将直接退出程序' : '已保存：关闭按钮将缩小到托盘',
    )
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingCloseAction.value = false
  }
}
</script>

<style scoped>
.cfg-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-width: 720px;
}

.cfg-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  background: var(--panel-bg);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  padding: 12px 14px;
}

.cfg-info {
  min-width: 0;
}

.cfg-label {
  font-size: 13.5px;
  font-weight: 600;
}

.cfg-desc {
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 3px;
}

.cfg-ctrl {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.cfg-note {
  font-size: 12px;
  color: #4a5060;
}
</style>