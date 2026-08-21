<template>
    <el-dialog v-model="updateState.visible" title="发现新版本" width="540px" :close-on-click-modal="false"
        :show-close="updateState.phase !== 'downloading'">
        <div v-if="updateState.phase === 'checking'" class="upd-center">
            <el-icon class="is-loading" :size="24"><Loading /></el-icon>
            <span>正在检查更新…</span>
        </div>

        <template v-else-if="updateState.info">
            <div class="upd-versions">
                <span class="upd-v">当前版本 <b>{{ updateState.info.current }}</b></span>
                <el-icon class="upd-arrow"><Right /></el-icon>
                <span class="upd-v">最新版本 <b class="new">{{ updateState.info.latest }}</b></span>
            </div>
            <div v-if="updateState.info.name" class="upd-name">{{ updateState.info.name }}</div>
            <div v-if="updateState.info.body" class="upd-notes">
                <div class="upd-notes-title">更新内容：</div>
                <pre class="upd-body">{{ updateState.info.body.slice(0, 900) }}{{ updateState.info.body.length > 900 ? '…' : '' }}</pre>
            </div>

            <!-- 下载进度 -->
            <div v-if="updateState.phase === 'downloading'" class="upd-block">
                <el-progress :percentage="percent" :stroke-width="8" />
                <div class="upd-size">
                    {{ formatBytes(updateState.done) }} / {{ formatBytes(updateState.total) }}
                </div>
            </div>
            <div v-if="updateState.downloadError" class="upd-err">
                下载失败：{{ updateState.downloadError }}
            </div>
            <div v-if="updateState.downloadedPath" class="upd-done">
                <el-icon color="#34c759"><CircleCheckFilled /></el-icon>
                <span class="upd-path" :title="updateState.downloadedPath">
                    已下载到：{{ updateState.downloadedPath }}
                </span>
            </div>
        </template>

        <template #footer>
            <el-button v-if="updateState.phase === 'ready'" @click="closeUpdateDialog">稍后</el-button>
            <el-button v-if="updateState.phase === 'ready'" type="primary" @click="downloadUpdate">
                下载更新
            </el-button>
            <template v-if="updateState.downloadedPath">
                <el-button type="primary" @click="revealDownload">打开所在文件夹</el-button>
                <el-button @click="launchDownload">运行新版本</el-button>
                <el-button @click="openReleasePage">查看发布页</el-button>
            </template>
            <el-button v-if="updateState.downloadError" @click="openReleasePage">查看发布页</el-button>
            <el-button v-if="updateState.downloadError" @click="closeUpdateDialog">关闭</el-button>
        </template>
    </el-dialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Loading, Right, CircleCheckFilled } from '@element-plus/icons-vue'
import {
    updateState,
    closeUpdateDialog,
    downloadUpdate,
    revealDownload,
    launchDownload,
    openReleasePage,
    formatBytes,
} from '../utils/updateCheck'

const percent = computed(() => {
    if (!updateState.total) return 0
    return Math.min(100, Math.round((updateState.done / updateState.total) * 100))
})
</script>

<style scoped>
.upd-center {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 30px 0;
    color: var(--text-secondary);
    font-size: 13px;
}

.upd-versions {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
}

.upd-arrow {
    color: var(--text-secondary);
}

.upd-v b {
    font-weight: 600;
}

.upd-v b.new {
    color: #34c759;
}

.upd-name {
    margin-top: 8px;
    font-size: 12.5px;
    color: var(--text-primary);
}

.upd-notes {
    margin-top: 12px;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    background: #161a21;
    padding: 8px 10px;
    max-height: 220px;
    overflow-y: auto;
}

.upd-notes-title {
    font-size: 12px;
    color: var(--text-secondary);
    margin-bottom: 6px;
}

.upd-body {
    margin: 0;
    font-size: 12px;
    line-height: 1.7;
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--text-primary);
    font-family: inherit;
}

.upd-block {
    margin-top: 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
}

.upd-size {
    font-size: 11.5px;
    color: var(--text-secondary);
}

.upd-err {
    margin-top: 12px;
    font-size: 12.5px;
    color: #f56c6c;
}

.upd-done {
    margin-top: 12px;
    display: flex;
    align-items: flex-start;
    gap: 8px;
    font-size: 12.5px;
    color: #34c759;
}

.upd-path {
    word-break: break-all;
}
</style>
