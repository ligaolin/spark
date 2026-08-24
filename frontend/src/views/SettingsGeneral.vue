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
        <div class="cfg-label">网络面板自动刷新间隔（秒）</div>
        <div class="cfg-desc">网络信息（接口 / 端口 / 路由 / DNS）定时刷新的频率，设置后立即生效</div>
      </div>
      <div class="cfg-ctrl">
        <el-input-number v-model="networkVal" :min="1" :max="300" :step="1" size="small" />
        <el-button size="small" type="primary" :loading="savingNetwork" @click="saveNetwork">
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
        <div class="cfg-label">编辑器自动换行</div>
        <div class="cfg-desc">代码 / 文本编辑器（文档管理、远程文件编辑）中长行是否折行显示；关闭时出现横向滚动条。保存后立即对已打开的编辑器生效</div>
      </div>
      <div class="cfg-ctrl">
        <el-switch v-model="wordWrapVal" />
        <el-button size="small" type="primary" :loading="savingWordWrap" @click="saveWordWrap">
          保存
        </el-button>
      </div>
    </div>

    <div class="cfg-row">
      <div class="cfg-info">
        <div class="cfg-label">编辑器选中分隔符</div>
        <div class="cfg-desc">
          双击选中单词时的截断字符。<b>留空 = 连选</b>：除空格、回车（换行）外全部可连选，
          如 <span class="mono-inline">user_name</span>、<span class="mono-inline">user.name</span> 都能整体选中。
          填写分隔符（如 <span class="mono-inline">_</span> 或 <span class="mono-inline">._-</span>）后，
          只有列出的字符处截断，空格、换行始终截断无需配置。保存后立即对已打开的编辑器生效
        </div>
      </div>
      <div class="cfg-ctrl">
        <el-input v-model="wordSepVal" size="small" style="width: 120px" maxlength="32"
          placeholder="留空=默认" clearable />
        <el-button size="small" type="primary" :loading="savingWordSep" @click="saveWordSep">
          保存
        </el-button>
      </div>
    </div>

    <div class="cfg-row">
      <div class="cfg-info">
        <div class="cfg-label">编辑器切换标签自动定位文件</div>
        <div class="cfg-desc">
          在编辑器（本地文件夹 / SFTP / FTP 用编辑器打开目录）里切换已打开文件的标签时，
          左侧文件树自动展开所在目录、选中并滚动到该文件；关闭后仅切换标签不移动树。
          保存后立即生效
        </div>
      </div>
      <div class="cfg-ctrl">
        <el-switch v-model="treeFollowVal" />
        <el-button size="small" type="primary" :loading="savingTreeFollow" @click="saveTreeFollow">
          保存
        </el-button>
      </div>
    </div>

    <div class="cfg-row">
      <div class="cfg-info">
        <div class="cfg-label">搜索排除</div>
        <div class="cfg-desc">
          文件搜索（本地 / SFTP / FTP）默认排除的 glob 模式，逗号或换行分隔，如
          <span class="mono-inline">node_modules, dist, *.log</span>。
          搜索面板里填写的排除项会与这里叠加生效；排除目录会跳过整个子树。保存后立即生效
        </div>
      </div>
      <div class="cfg-ctrl">
        <el-input v-model="searchExcludeVal" size="small" style="width: 260px"
          placeholder="node_modules, dist, *.log" clearable type="textarea" autosize />
        <el-button size="small" type="primary" :loading="savingSearchExclude" @click="saveSearchExclude">
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

    <div class="cfg-row">
      <div class="cfg-info">
        <div class="cfg-label">开机启动</div>
        <!-- <div class="cfg-desc">登录系统后自动启动 Spark（Windows 写入注册表启动项，macOS 写入 LaunchAgent，Linux 写入 autostart）</div> -->
      </div>
      <div class="cfg-ctrl">
        <el-switch v-model="autoStartVal" :loading="savingAutoStart" @change="saveAutoStart" />
      </div>
    </div>

    <div class="cfg-note">更多配置项会陆续加到这里（配置存于本地数据库 settings 表）。</div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useSettingsStore } from '../stores/settings'
import { SettingsService } from '../utils/wails'

const settings = useSettingsStore()

const keepaliveVal = ref(20)
const processVal = ref(5)
const networkVal = ref(5)
const fontVal = ref(13)
const wordWrapVal = ref(true)
const wordSepVal = ref('')
const treeFollowVal = ref(true)
const searchExcludeVal = ref('')
const closeActionVal = ref<'minimize' | 'exit'>('minimize')
const autoStartVal = ref(false)
const savingKeepalive = ref(false)
const savingProcess = ref(false)
const savingNetwork = ref(false)
const savingFont = ref(false)
const savingWordWrap = ref(false)
const savingWordSep = ref(false)
const savingTreeFollow = ref(false)
const savingSearchExclude = ref(false)
const savingCloseAction = ref(false)
const savingAutoStart = ref(false)

onMounted(async () => {
  await settings.load()
  keepaliveVal.value = settings.keepaliveInterval
  processVal.value = settings.processRefreshInterval
  networkVal.value = settings.networkRefreshInterval
  fontVal.value = settings.terminalFontSize
  wordWrapVal.value = settings.editorWordWrap
  wordSepVal.value = settings.editorWordSeparators
  treeFollowVal.value = settings.editorTreeFollow
  searchExcludeVal.value = settings.searchExclude
  closeActionVal.value = settings.windowCloseAction
  autoStartVal.value = await SettingsService.IsAutoStart()
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

async function saveNetwork() {
  savingNetwork.value = true
  try {
    await settings.set('network.refresh', String(networkVal.value))
    ElMessage.success('已保存')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingNetwork.value = false
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

async function saveWordWrap() {
  savingWordWrap.value = true
  try {
    await settings.set('editor.wordWrap', wordWrapVal.value ? '1' : '0')
    ElMessage.success('已保存（已打开的编辑器立即生效）')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingWordWrap.value = false
  }
}

async function saveWordSep() {
  savingWordSep.value = true
  try {
    await settings.set('editor.wordSeparators', wordSepVal.value)
    ElMessage.success('已保存（已打开的编辑器立即生效）')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingWordSep.value = false
  }
}

async function saveTreeFollow() {
  savingTreeFollow.value = true
  try {
    await settings.set('editor.treeFollow', treeFollowVal.value ? '1' : '0')
    ElMessage.success(treeFollowVal.value ? '已保存：切换标签将自动定位文件' : '已保存：切换标签不再定位文件')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingTreeFollow.value = false
  }
}

async function saveSearchExclude() {
  savingSearchExclude.value = true
  try {
    await settings.set('search.exclude', searchExcludeVal.value.trim())
    ElMessage.success('已保存（搜索立即生效）')
  } catch (e: any) {
    ElMessage.error(`保存失败：${e?.message || e}`)
  } finally {
    savingSearchExclude.value = false
  }
}

async function saveAutoStart(v: boolean) {
  savingAutoStart.value = true
  try {
    await SettingsService.SetAutoStart(v)
    ElMessage.success(v ? '已开启开机启动' : '已关闭开机启动')
  } catch (e: any) {
    autoStartVal.value = !v
    ElMessage.error(`设置失败：${e?.message || e}`)
  } finally {
    savingAutoStart.value = false
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
  color: var(--text-muted);
}

.mono-inline {
  font-family: var(--term-font);
  background: var(--hover-bg);
  border-radius: 3px;
  padding: 0 3px;
}
</style>