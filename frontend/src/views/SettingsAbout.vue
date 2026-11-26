<template>
  <div class="about-card">
    <img class="about-logo" :src="logoUrl" alt="Spark" />
    <div class="about-name">Spark 终端</div>
    <div class="about-version">版本 {{ version }}</div>
    <div class="about-desc">
      跨平台 SSH / SFTP / FTP 终端工具：多标签终端、跳板机、文件管理、
      服务器监控、AI 助手与站点管理，数据本地加密保存。
    </div>
    <div class="about-links">
      <el-link type="primary" href="https://github.com/ligaolin/spark" target="_blank">
        github.com/ligaolin/spark
      </el-link>
    </div>
    <div class="about-actions">
      <el-button size="small" type="primary" plain @click="checkUpdate">检查更新</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { SettingsService } from '../utils/wails'
import { checkForUpdatesManual } from '../utils/updateCheck'
import logoUrl from '../assets/logo.png'

const version = ref('dev')

onMounted(async () => {
  try {
    version.value = await SettingsService.GetVersion()
  } catch {
    /* 忽略 */
  }
})

function checkUpdate() {
  void checkForUpdatesManual()
}
</script>

<style scoped>
.about-card {
  max-width: 560px;
  background: var(--panel-bg);
  border: 1px solid var(--border-color);
  border-radius: 10px;
  padding: 28px 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  text-align: center;
}

.about-logo {
  width: 64px;
  height: 64px;
  border-radius: 14px;
}

.about-name {
  font-size: 18px;
  font-weight: 700;
}

.about-version {
  font-size: 13px;
  color: var(--text-secondary);
}

.about-desc {
  font-size: 12.5px;
  color: var(--text-secondary);
  line-height: 1.9;
}

.about-links {
  margin-top: 8px;
}

.about-actions {
  margin-top: 12px;
}
</style>