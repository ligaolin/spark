<template>
  <div class="settings-view">
    <div class="settings-head">
      <span class="settings-title">设置</span>
    </div>

    <el-tabs v-model="tab" class="settings-tabs">
      <el-tab-pane label="常规" name="general">
        <SettingsGeneral />
      </el-tab-pane>
      <el-tab-pane label="快捷键" name="shortcuts">
        <SettingsShortcuts />
      </el-tab-pane>
      <el-tab-pane label="同步" name="database">
        <SettingsDatabase />
      </el-tab-pane>
      <el-tab-pane label="关于" name="about">
        <SettingsAbout />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import SettingsGeneral from './SettingsGeneral.vue'
import SettingsShortcuts from './SettingsShortcuts.vue'
import SettingsDatabase from './SettingsDatabase.vue'
import SettingsAbout from './SettingsAbout.vue'

const tab = ref('general')
</script>

<style scoped>
.settings-view {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 10px 14px;
  gap: 10px;
  overflow-y: auto;
}

.settings-head {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.settings-title {
  font-size: 15px;
  font-weight: 600;
}

.settings-tabs {
  flex: 1;
  min-height: 0;
}
</style>