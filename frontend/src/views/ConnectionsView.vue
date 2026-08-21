<template>
    <div class="conn-view">
        <div class="conn-head">
            <span class="conn-title">连接管理</span>
            <div class="head-actions">
                <el-button size="small" @click="openHostKeys">
                    <el-icon style="margin-right: 4px">
                        <Key />
                    </el-icon>
                    主机密钥
                </el-button>
                <el-button size="small" @click="sshImportVisible = true">
                    <el-icon style="margin-right: 4px">
                        <Upload />
                    </el-icon>
                    导入 SSH Config
                </el-button>
                <el-button size="small" type="warning" plain @click="dedupVisible = true">
                    <el-icon style="margin-right: 4px">
                        <Remove />
                    </el-icon>
                    移除重复
                </el-button>
                <el-button type="primary" size="small" @click="openCreate">
                    <el-icon style="margin-right: 4px">
                        <Plus />
                    </el-icon>
                    新建连接
                </el-button>
            </div>
        </div>

        <div class="conn-body">
            <!-- 分组侧栏 -->
            <aside class="group-side">
                <div class="group-side-head">
                    <span class="group-side-title">分组</span>
                    <el-button size="small" text type="primary" @click="createGroup">
                        <el-icon style="margin-right: 3px">
                            <Plus />
                        </el-icon>
                        新建
                    </el-button>
                </div>
                <div class="group-list">
                    <div class="group-item" :class="{ active: selectedGroup === ALL }" @click="selectedGroup = ALL">
                        <el-icon class="group-icon">
                            <Menu />
                        </el-icon>
                        <span class="group-name">全部</span>
                        <span class="group-count">{{ connStore.list.length }}</span>
                    </div>
                    <div class="group-item" :class="{ active: selectedGroup === NONE }" @click="selectedGroup = NONE">
                        <el-icon class="group-icon">
                            <Folder />
                        </el-icon>
                        <span class="group-name">未分组</span>
                        <span class="group-count">{{ noneCount }}</span>
                    </div>
                    <div v-for="g in connStore.groups" :key="g.name" class="group-item"
                        :class="{ active: selectedGroup === g.name }" @click="selectedGroup = g.name">
                        <el-icon class="group-icon">
                            <Folder />
                        </el-icon>
                        <span class="group-name" :title="g.name">{{ g.name }}</span>
                        <span class="group-count">{{ groupCount(g.name) }}</span>
                        <span class="group-actions">
                            <el-icon title="重命名" @click.stop="renameGroup(g.name)">
                                <Edit />
                            </el-icon>
                            <el-icon title="删除分组" class="danger" @click.stop="removeGroup(g.name)">
                                <Delete />
                            </el-icon>
                        </span>
                    </div>
                </div>
            </aside>

            <!-- 连接表格 -->
            <div class="conn-main">
                <el-alert v-if="undecryptedCount > 0" type="warning" :closable="false" show-icon class="decrypt-warn"
                    :title="`${undecryptedCount} 个连接的密码无法解密（可能同步密钥不匹配，或数据来自设置了其他密钥的机器）`"
                    description="请在「设置 → 数据库」确认同步密钥与来源机器一致；或编辑这些连接重新填写密码（保存后会用当前密钥重新加密）。" />
                <el-table :data="filteredList" size="small" :loading="connStore.loading" empty-text="还没有保存的连接">
                    <el-table-column label="名称">
                        <template #default="{ row }">
                            <span class="conn-name">{{ row.name }}</span>
                        </template>
                    </el-table-column>
                    <el-table-column label="分组" width="180">
                        <template #default="{ row }">
                            <el-select :model-value="row.group" size="small" filterable allow-create clearable
                                default-first-option placeholder="未分组" @change="(v: string) => setRowGroup(row, v)">
                                <el-option v-for="g in connStore.groupNames" :key="g" :label="g" :value="g" />
                            </el-select>
                        </template>
                    </el-table-column>
                    <el-table-column label="类型" width="60">
                        <template #default="{ row }">
                            <el-tag :type="row.type === 'ssh' ? 'primary' : 'warning'" size="small" effect="dark">
                                {{ row.type.toUpperCase() }}
                            </el-tag>
                        </template>
                    </el-table-column>
                    <el-table-column label="地址">
                        <template #default="{ row }">
                            <span class="mono dim">{{ row.host }}:{{ row.port }}</span>
                        </template>
                    </el-table-column>
                    <el-table-column label="用户名">
                        <template #default="{ row }">
                            <span class="dim">{{ row.username }}</span>
                        </template>
                    </el-table-column>
                    <el-table-column label="认证" width="60" align="center">
                        <template #default="{ row }">
                            <el-tag v-if="row.useKey" size="small" type="info">密钥</el-tag>
                            <el-tag v-else size="small" type="info" effect="plain">密码</el-tag>
                        </template>
                    </el-table-column>
                    <el-table-column label="操作" align="right" width="180">
                        <template #default="{ row }">
                            <div class="conn-actions">
                                <el-button v-if="row.type === 'ssh'" size="small" type="primary" plain
                                    @click="openTerminal(row)">ssh</el-button>
                                <el-button v-else size="small" @click="openFtp(row)">FTP</el-button>
                                <el-button size="small" @click="openEdit(row)">编辑</el-button>
                                <el-button size="small" type="danger" plain @click="remove(row)">删除</el-button>
                            </div>
                        </template>
                    </el-table-column>
                </el-table>
            </div>
        </div>

        <ConnectDialog v-model="dialogVisible" :mode="dialogMode" :conn-type="dialogType" :connection="editing"
            :groups="connStore.groupNames" @saved="onSaved" @connect="onQuickConnect" />

        <SshConfigImport v-model="sshImportVisible" :groups="connStore.groupNames" @imported="onImported" />

        <!-- 主机密钥管理 -->
        <el-dialog v-model="hostKeyVisible" title="已保存的主机密钥（known_hosts）" width="640px">
            <el-table :data="hostKeys" size="small" empty-text="还没有保存任何主机密钥">
                <el-table-column label="主机" width="170">
                    <template #default="{ row }">
                        <span class="mono">{{ row.host }}</span>
                    </template>
                </el-table-column>
                <el-table-column label="端口" width="70">
                    <template #default="{ row }">
                        <span class="dim">{{ row.port }}</span>
                    </template>
                </el-table-column>
                <el-table-column label="密钥类型" width="110">
                    <template #default="{ row }">
                        <el-tag size="small" type="info">{{ row.keyType }}</el-tag>
                    </template>
                </el-table-column>
                <el-table-column label="指纹（SHA256）" min-width="220">
                    <template #default="{ row }">
                        <span class="mono dim">{{ row.fingerprint }}</span>
                    </template>
                </el-table-column>
                <el-table-column label="操作" width="80" align="right">
                    <template #default="{ row }">
                        <el-button size="small" type="danger" plain @click="removeHostKey(row)">删除</el-button>
                    </template>
                </el-table-column>
            </el-table>
            <template #footer>
                <el-button @click="hostKeyVisible = false">关闭</el-button>
            </template>
        </el-dialog>

        <!-- 移除重复 -->
        <el-dialog v-model="dedupVisible" title="移除重复连接" width="480px" :close-on-click-modal="false">
            <div class="dedup-body">
                <p class="dedup-desc">
                    将按「主机地址 + 类型」检测重复，每组保留一条，其余删除（不可撤销）。
                    优先保留可用的连接；若都不通则保留最后创建的一条。
                </p>
                <el-checkbox v-model="dedupDeep">
                    尝试登录校验（更准确地保留凭据仍可用的连接，但较慢，且可能触发服务器的登录失败限制）
                </el-checkbox>
            </div>
            <template #footer>
                <el-button @click="dedupVisible = false">取消</el-button>
                <el-button type="warning" :loading="dedupRunning" @click="runDedup">开始移除</el-button>
            </template>
        </el-dialog>
    </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Key, Edit, Delete, Folder, Menu, Upload, Remove } from '@element-plus/icons-vue'
import { showConfirmDialog, showInputDialog, showAlertDialog } from '../utils/dialog'
import ConnectDialog from '../components/ConnectDialog.vue'
import SshConfigImport from '../components/SshConfigImport.vue'
import { useConnectionsStore } from '../stores/connections'
import { useTerminalStore } from '../stores/terminal'
import { makeConnectOptions, makeSavedConnection, HostKeyService } from '../utils/wails'
import type { ConnectOptions, SavedConnection, HostKeyInfo } from '../utils/wails'

const router = useRouter()
const connStore = useConnectionsStore()
const termStore = useTerminalStore()

// 分组筛选哨兵值
const ALL = '__all__'
const NONE = '__none__'

// 检测密码/私钥未解密的连接（同步密钥不匹配等）
const undecryptedCount = computed(
    () =>
        connStore.list.filter(
            (c) => c.password.startsWith('enc:') || c.privateKey.startsWith('enc:'),
        ).length,
)

const selectedGroup = ref<string>(ALL)

const filteredList = computed(() => {
    if (selectedGroup.value === ALL) return connStore.list
    if (selectedGroup.value === NONE) return connStore.list.filter((c) => !c.group)
    return connStore.list.filter((c) => c.group === selectedGroup.value)
})

const noneCount = computed(() => connStore.list.filter((c) => !c.group).length)

function groupCount(name: string) {
    return connStore.list.filter((c) => c.group === name).length
}

const dialogVisible = ref(false)
const dialogMode = ref<'create' | 'edit'>('create')
const dialogType = ref<'ssh' | 'ftp'>('ssh')
const editing = ref<SavedConnection | null>(null)

const hostKeyVisible = ref(false)
const hostKeys = ref<HostKeyInfo[]>([])

const sshImportVisible = ref(false)

const dedupVisible = ref(false)
const dedupDeep = ref(false)
const dedupRunning = ref(false)

onMounted(() => connStore.load())

function onImported() {
    connStore.load()
}

async function openHostKeys() {
    hostKeyVisible.value = true
    await loadHostKeys()
}

async function loadHostKeys() {
    try {
        hostKeys.value = (await HostKeyService.ListHostKeys()) ?? []
    } catch (e: any) {
        ElMessage.error(`读取主机密钥失败：${e?.message || e}`)
    }
}

async function removeHostKey(row: HostKeyInfo) {
    const ok = await showConfirmDialog(
        '删除主机密钥',
        `确定删除 ${row.host}:${row.port} 的主机密钥？下次连接会再次提示信任。`,
        true,
        '删除',
    )
    if (!ok) return
    try {
        await HostKeyService.RemoveHostKey(row.host, row.port)
        ElMessage.success('已删除')
        await loadHostKeys()
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

async function createGroup() {
    const values = await showInputDialog('新建分组', [
        { key: 'name', label: '分组名称', placeholder: '例如：生产环境' },
    ])
    if (!values) return
    const name = (values.name ?? '').trim()
    if (!name) return
    try {
        await connStore.createGroup(name)
        ElMessage.success('已创建分组')
    } catch (e: any) {
        ElMessage.error(`创建分组失败：${e?.message || e}`)
    }
}

async function renameGroup(name: string) {
    const values = await showInputDialog('重命名分组', [
        { key: 'name', label: '分组名称', initial: name },
    ])
    if (!values) return
    const newName = (values.name ?? '').trim()
    if (!newName || newName === name) return
    try {
        await connStore.renameGroup(name, newName)
        if (selectedGroup.value === name) selectedGroup.value = newName
        ElMessage.success('已重命名分组')
    } catch (e: any) {
        ElMessage.error(`重命名失败：${e?.message || e}`)
    }
}

async function removeGroup(name: string) {
    const count = groupCount(name)
    const ok = await showConfirmDialog(
        '删除分组',
        `确定删除分组「${name}」？分组内的 ${count} 个连接将移到「未分组」。`,
        true,
        '删除',
    )
    if (!ok) return
    try {
        await connStore.deleteGroup(name)
        if (selectedGroup.value === name) selectedGroup.value = ALL
        ElMessage.success('已删除分组')
    } catch (e: any) {
        ElMessage.error(`删除分组失败：${e?.message || e}`)
    }
}

async function setRowGroup(row: SavedConnection, group: string) {
    const g = group ? group.trim() : ''
    try {
        await connStore.setGroup(row.id!, g)
    } catch (e: any) {
        ElMessage.error(`移动分组失败：${e?.message || e}`)
    }
}

function openCreate() {
    dialogMode.value = 'create'
    dialogType.value = 'ssh'
    editing.value = null
    dialogVisible.value = true
}

function openEdit(conn: SavedConnection) {
    dialogMode.value = 'edit'
    dialogType.value = conn.type === 'ftp' ? 'ftp' : 'ssh'
    editing.value = { ...conn }
    dialogVisible.value = true
}

async function onSaved(conn: SavedConnection) {
    dialogVisible.value = false
    try {
        if (dialogMode.value === 'create') {
            await connStore.create(conn)
            ElMessage.success('已创建连接')
        } else {
            await connStore.update(conn)
            ElMessage.success('已保存修改')
        }
    } catch (e: any) {
        ElMessage.error(`保存失败：${e?.message || e}`)
    }
}

async function onQuickConnect(opts: ConnectOptions, save: boolean) {
    dialogVisible.value = false
    if (save) {
        try {
            await connStore.create(
                makeSavedConnection({
                    name: `${opts.username}@${opts.host}:${opts.port}`,
                    type: dialogType.value,
                    host: opts.host,
                    port: opts.port,
                    username: opts.username,
                    password: opts.password,
                    useKey: opts.useKey,
                    privateKey: opts.privateKey,
                    passphrase: opts.passphrase,
                    defaultDir: opts.defaultDir || '',
                    tls: !!opts.tls,
                }),
            )
            ElMessage.success('已保存到连接列表')
        } catch (e: any) {
            ElMessage.error(`保存失败：${e?.message || e}`)
        }
    }
}

function connToOpts(conn: SavedConnection): ConnectOptions {
    return makeConnectOptions({
        host: conn.host,
        port: conn.port,
        username: conn.username,
        password: conn.password,
        useKey: conn.useKey,
        privateKey: conn.privateKey,
        passphrase: conn.passphrase,
        defaultDir: conn.defaultDir,
        tls: conn.tls,
    })
}

function openTerminal(conn: SavedConnection) {
    termStore.addTab(connToOpts(conn), conn.id ?? undefined)
    // 通知终端页：展开右侧面板并切到 SFTP 页（打开 SSH 默认两个都打开）
    sessionStorage.setItem('spark:open-terminal-panel', '1')
    router.push('/terminal')
}

function openFtp(conn: SavedConnection) {
    sessionStorage.setItem('spark:auto-connect', JSON.stringify({ mode: 'ftp', conn }))
    router.push('/ftp')
}

async function remove(conn: SavedConnection) {
    const ok = await showConfirmDialog('删除连接', `确定删除连接「${conn.name}」？`, true, '删除')
    if (!ok) return
    try {
        await connStore.remove(conn.id!)
        ElMessage.success('已删除')
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

async function runDedup() {
    dedupRunning.value = true
    try {
        const res = await connStore.removeDuplicates(dedupDeep.value)
        dedupVisible.value = false
        if ((res.removed ?? 0) > 0) {
            ElMessage.success(`已移除 ${res.removed} 个重复连接`)
            const summary = (res.summary ?? []).map(escapeHtml).join('<br/>')
            if (summary) await showAlertDialog('移除结果', summary)
        } else {
            ElMessage.info('没有发现重复连接')
        }
    } catch (e: any) {
        ElMessage.error(`移除重复失败：${e?.message || e}`)
    } finally {
        dedupRunning.value = false
    }
}

function escapeHtml(s: string) {
    return s.replace(/[&<>"']/g, (c) =>
        ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] as string,
    )
}
</script>

<style scoped>
.dedup-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
}

.dedup-body :deep(.el-checkbox) {
    display: flex;
    align-items: baseline;
}

.dedup-body :deep(.el-checkbox__label) {
    white-space: normal;
    word-break: break-word;
    line-height: 22px;
}

.dedup-desc {
    margin: 0;
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.8;
}

.conn-view {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 10px 12px;
    gap: 10px;
}

.conn-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-shrink: 0;
}

.head-actions {
    display: flex;
    align-items: center;
    gap: 8px;
}

.conn-title {
    font-size: 15px;
    font-weight: 600;
}

.conn-body {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 12px;
}

.group-side {
    width: 200px;
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border-color);
    border-radius: 8px;
    background: #171a20;
    overflow: hidden;
}

.group-side-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
}

.group-side-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
}

.group-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 6px;
}

.group-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 8px;
    border-radius: 6px;
    color: var(--text-secondary);
    font-size: 13px;
    cursor: pointer;
    transition: background 0.15s, color 0.15s;
}

.group-item:hover {
    background: #1e222b;
    color: var(--text-primary);
}

.group-item.active {
    background: #233049;
    color: #7fb0ff;
}

.group-icon {
    font-size: 14px;
    flex-shrink: 0;
}

.group-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.group-count {
    font-size: 11px;
    color: var(--text-secondary);
    background: #22262f;
    border-radius: 9px;
    padding: 0 7px;
    line-height: 18px;
    flex-shrink: 0;
}

.group-item.active .group-count {
    background: #2c3c5c;
    color: #9cc0ff;
}

.group-actions {
    display: none;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
}

.group-item:hover .group-actions {
    display: inline-flex;
}

.group-actions .el-icon {
    font-size: 14px;
}

.group-actions .el-icon:hover {
    color: #7fb0ff;
}

.group-actions .el-icon.danger:hover {
    color: #f56c6c;
}

.conn-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.decrypt-warn {
    flex-shrink: 0;
}

.conn-actions {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
}

.conn-name {
    font-weight: 500;
}

.dim {
    color: var(--text-secondary);
}
</style>