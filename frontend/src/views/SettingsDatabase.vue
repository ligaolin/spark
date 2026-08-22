<template>
    <el-form ref="formRef" :model="dbForm" label-width="120px" label-position="left" class="db-form">
        <el-form-item label="当前存储">
            <el-tag size="small" :type="currentIsRemote ? 'warning' : 'info'" effect="dark">
                {{ currentLabel }}
            </el-tag>
        </el-form-item>

        <el-form-item label="存储方式">
            <el-radio-group v-model="storageMode">
                <el-radio value="local">本地 SQLite</el-radio>
                <el-radio value="remote">远程数据库（多机同步）</el-radio>
            </el-radio-group>
        </el-form-item>

        <template v-if="storageMode === 'remote'">
            <el-form-item label="数据库类型">
                <el-select v-model="dbForm.dialect" style="width: 300px" @change="onDialectChange">
                    <el-option value="mysql" label="MySQL" />
                    <el-option value="postgres" label="PostgreSQL" />
                    <el-option value="sqlserver" label="SQL Server" />
                    <el-option value="oracle" label="Oracle（纯 Go 驱动）" />
                </el-select>
            </el-form-item>

            <el-form-item label="主机">
                <el-input v-model="dbForm.host" placeholder="IP 或域名" style="width: 300px" />
            </el-form-item>

            <el-form-item label="端口">
                <el-input-number v-model="dbForm.port" :min="1" :max="65535" style="width: 160px" />
            </el-form-item>

            <el-form-item label="用户名">
                <el-input v-model="dbForm.username" style="width: 300px" />
            </el-form-item>

            <el-form-item label="密码">
                <el-input v-model="dbForm.password" type="password" show-password style="width: 300px" />
            </el-form-item>

            <el-form-item label="数据库名">
                <el-input v-model="dbForm.database"
                    :placeholder="dbForm.dialect === 'oracle' ? '服务名 SERVICE_NAME，如 XE' : '需要提前创建好'"
                    style="width: 300px" />
            </el-form-item>

            <el-form-item label="附加参数">
                <el-input v-model="dbForm.params" placeholder="可选，如 sslmode=require" style="width: 300px" />
            </el-form-item>

            <el-form-item label="同步密钥">
                <el-input v-model="dbForm.syncKey" type="password" show-password placeholder="可选（高级）；默认不需要填写"
                    style="width: 300px" />
            </el-form-item>

            <!-- <el-form-item label=" ">
          <el-alert
            type="success"
            :closable="false"
            show-icon
            title="远程库自动互通，无需额外设置"
            description="连接密码的加密密钥由数据库连接信息自动派生：任何能连接同一数据库的机器都能自动解密，开箱即用。"
          />
        </el-form-item> -->

            <el-form-item label=" ">
                <div class="db-note">
                    同步密钥是<b>可选的高级加固</b>：只有所有机器都填写了完全相同的密钥，才能解密连接密码。
                    一般不填即可。数据库连接信息（含密码）保存在本机配置文件中。
                </div>
            </el-form-item>
        </template>

        <el-form-item label=" ">
            <div class="db-backup">
                <div class="db-backup-head">备份 / 迁移连接配置</div>
                <div class="db-backup-actions">
                    <el-button size="small" :loading="exporting" @click="exportConn">
                        <el-icon><CopyDocument /></el-icon>复制连接串
                    </el-button>
                    <el-button size="small" :loading="importing" @click="importConn">
                        <el-icon><Download /></el-icon>导入连接串
                    </el-button>
                </div>
                <div class="db-note">
                    安卓端<b>重装后</b>本机保存的连接配置会丢失：重装前先「复制连接串」保存到微信 / 笔记，
                    重装后「导入连接串」粘贴即可自动填好连接信息，无需重新输入。
                    桌面端换电脑同样适用。连接串包含数据库密码，请妥善保管。
                </div>
            </div>
        </el-form-item>

        <!-- <template v-else>
        <el-form-item label=" ">
          <div class="db-note">数据保存在本机 SQLite 文件（gorm.db）中。</div>
        </el-form-item>
      </template> -->

        <el-form-item>
            <div class="db-actions">
                <el-button :loading="testing" @click="testConnection">测试连接</el-button>
                <el-button type="primary" :loading="switching" @click="switchStorage">
                    保存并切换
                </el-button>
            </div>
        </el-form-item>
    </el-form>
</template>

<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Clipboard } from '@wailsio/runtime'
import { CopyDocument, Download } from '@element-plus/icons-vue'
import { DatabaseService, makeDatabaseConfig } from '../utils/wails'
import { useSettingsStore } from '../stores/settings'
import { useShortcutsStore } from '../stores/shortcuts'
import { useConnectionsStore } from '../stores/connections'
import { useCustomCommandsStore } from '../stores/customCommands'
import { showInputDialog, showConfirmDialog } from '../utils/dialog'

const settings = useSettingsStore()
const shortcuts = useShortcutsStore()
const connStore = useConnectionsStore()
const cmdStore = useCustomCommandsStore()

const storageMode = ref<'local' | 'remote'>('local')
const currentIsRemote = ref(false)
const currentLabel = ref('本地 SQLite')
const dbForm = reactive({
    dialect: 'mysql',
    host: '',
    port: 3306,
    username: '',
    password: '',
    database: '',
    params: '',
    syncKey: '',
})
const testing = ref(false)
const switching = ref(false)
const exporting = ref(false)
const importing = ref(false)

const DIALECT_DEFAULTS: Record<string, { port: number; label: string }> = {
    mysql: { port: 3306, label: 'MySQL' },
    postgres: { port: 5432, label: 'PostgreSQL' },
    sqlserver: { port: 1433, label: 'SQL Server' },
    oracle: { port: 1521, label: 'Oracle' },
}

function onDialectChange(d: string) {
    dbForm.port = DIALECT_DEFAULTS[d]?.port ?? 3306
}

function buildConfig() {
    if (storageMode.value === 'local') {
        return makeDatabaseConfig({ dialect: 'sqlite', syncKey: dbForm.syncKey || '' })
    }
    return makeDatabaseConfig({
        dialect: dbForm.dialect,
        host: dbForm.host.trim(),
        port: dbForm.port,
        username: dbForm.username.trim(),
        password: dbForm.password,
        database: dbForm.database.trim(),
        params: dbForm.params.trim(),
        syncKey: dbForm.syncKey,
    })
}

async function loadCurrentDb() {
    try {
        const cfg = await DatabaseService.GetCurrent()
        if (cfg.dialect === 'sqlite' || !cfg.dialect) {
            storageMode.value = 'local'
            currentIsRemote.value = false
            currentLabel.value = '本地 SQLite'
        } else {
            storageMode.value = 'remote'
            currentIsRemote.value = true
            currentLabel.value = `${DIALECT_DEFAULTS[cfg.dialect]?.label || cfg.dialect} @ ${cfg.host}:${cfg.port || ''}`
            Object.assign(dbForm, {
                dialect: cfg.dialect,
                host: cfg.host,
                port: cfg.port || DIALECT_DEFAULTS[cfg.dialect]?.port || 3306,
                username: cfg.username,
                password: cfg.password,
                database: cfg.database,
                params: cfg.params,
                syncKey: cfg.syncKey,
            })
        }
    } catch {
        /* 忽略 */
    }
}

async function testConnection() {
    testing.value = true
    try {
        await DatabaseService.Test(buildConfig())
        ElMessage.success('连接成功')
    } catch (e: any) {
        ElMessage.error(`连接失败：${e?.message || e}`)
    } finally {
        testing.value = false
    }
}

// 导出当前连接配置为可分享的连接串（安卓重装 / 换电脑后导入恢复）
async function exportConn() {
    exporting.value = true
    try {
        const s = await DatabaseService.ExportConfig()
        let copied = false
        try {
            await Clipboard.SetText(s)
            copied = true
        } catch {
            /* 部分平台（如安卓）剪贴板 API 不可用，改为手动复制 */
        }
        // 无论是否复制成功都展示连接串：可长按选择复制、转发到微信/笔记
        await showInputDialog('连接串', [
            {
                key: 's',
                label: copied ? '已复制到剪贴板（也可长按手动复制）' : '请长按选择复制',
                type: 'textarea',
                initial: s,
                readonly: true,
                optional: true,
            },
        ])
        ElMessage.success(copied ? '已复制连接串到剪贴板' : '请长按选择复制连接串')
    } catch (e: any) {
        ElMessage.error(`导出失败：${e?.message || e}`)
    } finally {
        exporting.value = false
    }
}

// 导入连接串：解析后填入表单，由用户核对后点「保存并切换」
async function importConn() {
    const v = await showInputDialog('导入连接串', [
        {
            key: 's',
            label: '连接串',
            type: 'textarea',
            placeholder: '粘贴「复制连接串」导出的内容（sparkdb:// 开头）',
        },
    ])
    if (!v) return
    importing.value = true
    try {
        const cfg = await DatabaseService.ImportConfig(v.s)
        if (cfg.dialect === 'sqlite') {
            storageMode.value = 'local'
        } else {
            storageMode.value = 'remote'
            Object.assign(dbForm, {
                dialect: cfg.dialect,
                host: cfg.host,
                port: cfg.port || DIALECT_DEFAULTS[cfg.dialect]?.port || 3306,
                username: cfg.username,
                password: cfg.password,
                database: cfg.database,
                params: cfg.params,
                syncKey: cfg.syncKey,
            })
        }
        ElMessage.success('已导入连接信息，请核对后点击「保存并切换」')
    } catch (e: any) {
        ElMessage.error(`导入失败：${e?.message || e}`)
    } finally {
        importing.value = false
    }
}

async function switchStorage() {
    if (storageMode.value === 'remote' && !dbForm.host.trim()) {
        ElMessage.warning('请填写主机地址')
        return
    }
    if (storageMode.value === 'remote' && !dbForm.database.trim()) {
        ElMessage.warning('请填写数据库名')
        return
    }
    const target = storageMode.value === 'remote'
        ? `${DIALECT_DEFAULTS[dbForm.dialect]?.label} @ ${dbForm.host}:${dbForm.port}`
        : '本地 SQLite'
    const ok = await showConfirmDialog(
        '切换存储数据库',
        `确定切换到 <b>${target}</b>？<br>` +
        '远程库场景下连接密码自动按数据库连接加密，同一数据库的机器开箱即用（无需同步密钥）。' +
        '目标库为空时自动迁移当前数据；目标库已有数据（如多机共享）则只切换连接、不覆盖其内容。' +
        '切换后连接、收藏、命令、配置与快捷键都保存在该数据库，配置永久保存在本机，重启无需重新设置。',
        false,
        '切换',
    )
    if (!ok) return
    switching.value = true
    try {
        await DatabaseService.Switch(buildConfig())
        ElMessage.success('已切换并迁移完成')
        await loadCurrentDb()
        await settings.load()
        await shortcuts.load()
        await connStore.load()
        await cmdStore.load()
    } catch (e: any) {
        ElMessage.error(`切换失败：${e?.message || e}`)
    } finally {
        switching.value = false
    }
}

onMounted(async () => {
    await loadCurrentDb()
})
</script>

<style scoped>
.db-form {
    --el-form-label-font-size: 13px;
}

.db-form :deep(.el-form-item__label) {
    color: var(--text-primary);
    font-weight: 500;
}

.db-note {
    font-size: 12px;
    color: var(--text-secondary);
    line-height: 1.7;
}

.db-backup {
    width: 100%;
    border: 1px dashed var(--border-strong);
    border-radius: 6px;
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.db-backup-head {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
}

.db-backup-actions {
    display: flex;
    gap: 8px;
}

.db-actions {
    display: flex;
    gap: 8px;
}
</style>