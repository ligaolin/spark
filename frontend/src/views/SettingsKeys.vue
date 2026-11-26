<template>
    <div class="sk-page">
        <div class="sk-head">
            <span class="sk-tip">密钥仅保存在本机（私钥和口令加密存储），公钥可一键部署到任意已保存的服务器</span>
            <div class="sk-head-actions">
                <el-button size="small" @click="openImport">
                    <el-icon><Upload /></el-icon>
                    <span>导入私钥</span>
                </el-button>
                <el-button size="small" @click="openBatchImport">
                    <el-icon><Files /></el-icon>
                    <span>批量导入</span>
                </el-button>
                <el-button size="small" type="primary" @click="openGenerate">
                    <el-icon><Key /></el-icon>
                    <span>生成密钥</span>
                </el-button>
            </div>
        </div>

        <el-table :data="keys" size="small" empty-text="暂无密钥，点击右上角「生成密钥」" class="sk-table">
            <el-table-column label="名称" prop="name" min-width="140" show-overflow-tooltip />
            <el-table-column label="类型" width="90">
                <template #default="{ row }">
                    <el-tag size="small" type="info">{{ row.type }}</el-tag>
                </template>
            </el-table-column>
            <el-table-column label="指纹（SHA256）" min-width="260">
                <template #default="{ row }">
                    <span class="sk-fp mono" :title="row.fingerprint" @click="copyText(row.fingerprint, '指纹')">
                        {{ row.fingerprint }}
                    </span>
                </template>
            </el-table-column>
            <el-table-column label="备注" prop="comment" min-width="140" show-overflow-tooltip />
            <el-table-column label="创建时间" width="150">
                <template #default="{ row }">
                    <span class="dim">{{ formatTime(row.createdAt) }}</span>
                </template>
            </el-table-column>
            <el-table-column label="操作" width="280" fixed="right">
                <template #default="{ row }">
                    <el-button size="small" text type="primary" @click="openDeploy(row)">部署</el-button>
                    <el-button size="small" text @click="showPublic(row)">公钥</el-button>
                    <el-button size="small" text @click="showPrivate(row)">私钥</el-button>
                    <el-button size="small" text type="danger" @click="removeKey(row)">删除</el-button>
                </template>
            </el-table-column>
        </el-table>

        <!-- 生成密钥 -->
        <el-dialog v-model="generateVisible" title="生成 SSH 密钥" width="460px">
            <el-form label-width="90px" label-position="left">
                <el-form-item label="名称">
                    <el-input v-model="gen.name" placeholder="例如：我的笔记本" />
                </el-form-item>
                <el-form-item label="类型">
                    <el-radio-group v-model="gen.type">
                        <el-radio value="ed25519">Ed25519（推荐）</el-radio>
                        <el-radio value="rsa">RSA 4096</el-radio>
                    </el-radio-group>
                </el-form-item>
                <el-form-item label="备注">
                    <el-input v-model="gen.comment" placeholder="可选，追加在公钥末尾（如 user@host）" />
                </el-form-item>
            </el-form>
            <template #footer>
                <el-button @click="generateVisible = false">取消</el-button>
                <el-button type="primary" :loading="generating" @click="doGenerate">生成</el-button>
            </template>
        </el-dialog>

        <!-- 导入私钥 -->
        <el-dialog v-model="importVisible" title="导入 SSH 私钥" width="620px">
            <el-form label-width="90px" label-position="left">
                <el-form-item label="名称">
                    <el-input v-model="importForm.name" placeholder="例如：服务器 A 的旧密钥" />
                </el-form-item>
                <el-form-item label="私钥文件">
                    <div class="sk-row">
                        <el-input v-model="importForm.path" placeholder="选择文件或手动填写路径" class="mono" />
                        <el-button @click="pickKeyFile">选择文件</el-button>
                    </div>
                </el-form-item>
                <el-form-item label="口令">
                    <el-input v-model="importForm.passphrase" type="password" show-password
                        placeholder="私钥没有口令时留空" />
                </el-form-item>
                <el-form-item label="私钥内容">
                    <el-input v-model="importForm.privateKey" type="textarea" :rows="8"
                        placeholder="选择文件后自动填入，也可直接粘贴 PEM 内容" class="mono" />
                </el-form-item>
            </el-form>
            <div class="sk-import-note">
                公钥会从私钥自动推导并保存，无需单独导入；私钥和口令均加密存储在本机。
            </div>
            <template #footer>
                <el-button @click="importVisible = false">取消</el-button>
                <el-button type="primary" :loading="importing" @click="doImport">导入</el-button>
            </template>
        </el-dialog>

        <!-- 批量导入私钥 -->
        <el-dialog v-model="batchVisible" title="批量导入 SSH 私钥" width="560px"
            :close-on-click-modal="!batchImporting" :show-close="!batchImporting">
            <el-form label-width="90px" label-position="left">
                <el-form-item label="口令">
                    <el-input v-model="batchPassphrase" type="password" show-password :disabled="batchImporting"
                        placeholder="私钥都没有口令时留空" />
                </el-form-item>
            </el-form>
            <div class="sk-import-note">
                口令会应用于本次所有文件；口令不同的私钥请分别导入。公钥（.pub）文件会自动跳过。
            </div>
            <div class="sk-batch-list">
                <div v-for="f in batchItems" :key="f.path" class="sk-batch-item">
                    <span class="sk-batch-name" :title="f.path">{{ f.name }}</span>
                    <el-tag v-if="f.attempted && f.failed" size="small" type="danger">失败</el-tag>
                    <el-tag v-else-if="f.attempted" size="small" type="success">已导入</el-tag>
                    <span v-else class="dim sk-batch-wait">待导入</span>
                </div>
            </div>
            <div v-if="batchFailures.length && !batchImporting" class="sk-batch-fail">
                <div v-for="f in batchFailures" :key="f.path" class="sk-batch-fail-item">
                    {{ f.name }}：{{ f.error }}
                </div>
            </div>
            <div v-if="batchProgress" class="sk-batch-progress">{{ batchProgress }}</div>
            <template #footer>
                <el-button :disabled="batchImporting" @click="batchVisible = false">关闭</el-button>
                <el-button type="primary" :loading="batchImporting" @click="doBatchImport">
                    {{ batchFailures.length ? `重试失败（${batchFailures.length}）` : '开始导入' }}
                </el-button>
            </template>
        </el-dialog>

        <!-- 查看公钥 / 私钥 -->
        <el-dialog v-model="textVisible" :title="textTitle" width="640px">
            <el-alert v-if="textDanger" type="warning" :closable="false" show-icon
                title="私钥等同于密码，请勿泄露给他人" class="sk-alert" />
            <el-input :model-value="textValue" type="textarea" :rows="10" readonly class="mono" />
            <template #footer>
                <el-button type="primary" @click="copyText(textValue, '内容')">复制</el-button>
                <el-button @click="textVisible = false">关闭</el-button>
            </template>
        </el-dialog>

        <!-- 部署公钥 -->
        <el-dialog v-model="deployVisible" title="部署公钥到服务器" width="480px">
            <div class="sk-deploy-key">
                公钥：<span class="mono sk-deploy-fp">{{ deployKey?.fingerprint }}</span>
            </div>
            <el-form label-width="90px" label-position="left">
                <el-form-item label="目标服务器">
                    <el-select v-model="deployConnId" placeholder="选择已保存的 SSH 连接" style="width: 100%">
                        <el-option v-for="c in sshConns" :key="c.id" :value="c.id"
                            :label="`${c.name}（${c.username}@${c.host}${c.port && c.port !== 22 ? ':' + c.port : ''}）`" />
                    </el-select>
                </el-form-item>
            </el-form>
            <div class="sk-deploy-note">
                将把公钥追加到目标服务器的 <span class="mono">~/.ssh/authorized_keys</span>（已存在则跳过）。
            </div>
            <template #footer>
                <el-button @click="deployVisible = false">取消</el-button>
                <el-button type="primary" :loading="deploying" :disabled="!deployConnId" @click="doDeploy">部署</el-button>
            </template>
        </el-dialog>
    </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Files, Key, Upload } from '@element-plus/icons-vue'
import { Clipboard } from '@wailsio/runtime'
import { SSHKeyService, ConnService, LocalService, type SSHKey, type SavedConnection } from '../utils/wails'

const keys = ref<SSHKey[]>([])
const conns = ref<SavedConnection[]>([])
const sshConns = ref<SavedConnection[]>([])

const generateVisible = ref(false)
const generating = ref(false)
const gen = ref({ name: '', type: 'ed25519', comment: '' })

const importVisible = ref(false)
const importing = ref(false)
const importForm = ref({ name: '', path: '', passphrase: '', privateKey: '' })

// 批量导入
interface BatchItem {
    path: string
    name: string
    attempted: boolean
    failed: boolean
    error: string
}
const batchVisible = ref(false)
const batchImporting = ref(false)
const batchPassphrase = ref('')
const batchItems = ref<BatchItem[]>([])
const batchProgress = ref('')
const batchFailures = computed(() => batchItems.value.filter((i) => i.failed))

const textVisible = ref(false)
const textTitle = ref('')
const textValue = ref('')
const textDanger = ref(false)

const deployVisible = ref(false)
const deploying = ref(false)
const deployKey = ref<SSHKey | null>(null)
const deployConnId = ref<number>()

function formatTime(t: string): string {
    if (!t) return '-'
    const d = new Date(t)
    if (isNaN(d.getTime())) return '-'
    const p = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function loadKeys() {
    try {
        keys.value = (await SSHKeyService.List()) ?? []
    } catch (e: any) {
        ElMessage.error(`加载密钥失败：${e?.message || e}`)
    }
}

async function loadConns() {
    try {
        conns.value = (await ConnService.List()) ?? []
        sshConns.value = conns.value.filter((c) => c.type !== 'ftp' && c.host)
    } catch {
        // 静默：部署时才提示
    }
}

async function copyText(text: string, label: string) {
    try {
        await Clipboard.SetText(text)
        ElMessage.success(`已复制${label}`)
    } catch (e: any) {
        ElMessage.error(`复制失败：${e?.message || e}`)
    }
}

function openGenerate() {
    gen.value = { name: '', type: 'ed25519', comment: '' }
    generateVisible.value = true
}

async function doGenerate() {
    if (!gen.value.name.trim()) {
        ElMessage.warning('请输入名称')
        return
    }
    generating.value = true
    try {
        const key = await SSHKeyService.Generate(
            gen.value.name.trim(),
            gen.value.type,
            gen.value.type === 'rsa' ? 4096 : 0,
            gen.value.comment.trim(),
        )
        generateVisible.value = false
        await loadKeys()
        ElMessage.success('密钥已生成')
        showPublic(key)
    } catch (e: any) {
        ElMessage.error(`生成失败：${e?.message || e}`)
    } finally {
        generating.value = false
    }
}

function openImport() {
    importForm.value = { name: '', path: '', passphrase: '', privateKey: '' }
    importVisible.value = true
}

function baseName(p: string): string {
    const parts = p.split(/[\\/]/)
    return parts[parts.length - 1] || p
}

async function readKeyFile(path: string): Promise<string | null> {
    try {
        const text = await LocalService.ReadTextFile(path)
        importForm.value.privateKey = text
        if (!importForm.value.name.trim()) importForm.value.name = baseName(path)
        return text
    } catch (e: any) {
        ElMessage.error(`读取私钥文件失败：${e?.message || e}`)
        return null
    }
}

async function pickKeyFile() {
    try {
        const path = await LocalService.PickOpenFile('选择 SSH 私钥文件')
        if (!path) return
        importForm.value.path = path
        await readKeyFile(path)
    } catch (e: any) {
        ElMessage.error(`选择文件失败：${e?.message || e}`)
    }
}

async function doImport() {
    // 只填了路径没读内容时，按路径自动读取
    if (!importForm.value.privateKey.trim() && importForm.value.path.trim()) {
        if (!(await readKeyFile(importForm.value.path.trim()))) return
    }
    if (!importForm.value.name.trim()) {
        ElMessage.warning('请输入名称')
        return
    }
    if (!importForm.value.privateKey.trim()) {
        ElMessage.warning('请选择私钥文件或粘贴私钥内容')
        return
    }
    importing.value = true
    try {
        const key = await SSHKeyService.Import(
            importForm.value.name.trim(),
            importForm.value.privateKey,
            importForm.value.passphrase,
        )
        importVisible.value = false
        await loadKeys()
        ElMessage.success('已导入密钥')
        showPublic(key)
    } catch (e: any) {
        ElMessage.error(`导入失败：${e?.message || e}`)
    } finally {
        importing.value = false
    }
}

async function openBatchImport() {
    let files: string[]
    try {
        files = (await LocalService.PickFiles('选择 SSH 私钥文件（可多选）')) ?? []
    } catch (e: any) {
        ElMessage.error(`选择文件失败：${e?.message || e}`)
        return
    }
    if (!files.length) return
    const items: BatchItem[] = []
    let skipped = 0
    for (const p of files) {
        if (/\.pub$/i.test(p)) {
            skipped++
            continue
        }
        items.push({ path: p, name: baseName(p), attempted: false, failed: false, error: '' })
    }
    if (!items.length) {
        ElMessage.warning('所选文件都是公钥（.pub），请选择私钥文件')
        return
    }
    if (skipped) ElMessage.info(`已跳过 ${skipped} 个公钥文件`)
    batchItems.value = items
    batchPassphrase.value = ''
    batchProgress.value = ''
    batchVisible.value = true
}

async function doBatchImport() {
    // 首次导入全部；再次点击只重试失败的文件
    const targets = batchItems.value.filter((i) => !i.attempted || i.failed)
    if (!targets.length) return
    batchImporting.value = true
    try {
        for (let i = 0; i < targets.length; i++) {
            const f = targets[i]
            batchProgress.value = `正在导入 ${i + 1}/${targets.length}：${f.name}`
            f.attempted = true
            try {
                const text = await LocalService.ReadTextFile(f.path)
                if (!text || !text.trim()) throw new Error('文件内容为空')
                await SSHKeyService.Import(f.name, text, batchPassphrase.value)
                f.failed = false
                f.error = ''
            } catch (e: any) {
                f.failed = true
                f.error = e?.message || String(e)
            }
        }
    } finally {
        batchImporting.value = false
        batchProgress.value = ''
        await loadKeys()
    }
    const ok = batchItems.value.filter((i) => i.attempted && !i.failed).length
    if (!batchFailures.value.length) {
        batchVisible.value = false
        ElMessage.success(`已导入 ${ok} 个密钥`)
    } else {
        ElMessage.warning(`成功 ${ok} 个，失败 ${batchFailures.value.length} 个`)
    }
}

function showPublic(key: SSHKey) {
    textTitle.value = `公钥 · ${key.name}`
    textValue.value = key.publicKey
    textDanger.value = false
    textVisible.value = true
}

async function showPrivate(key: SSHKey) {
    try {
        await ElMessageBox.confirm('私钥等同于密码，确认查看？', '查看私钥', {
            confirmButtonText: '查看',
            cancelButtonText: '取消',
            type: 'warning',
        })
    } catch {
        return
    }
    textTitle.value = `私钥 · ${key.name}`
    textValue.value = key.privateKey
    textDanger.value = true
    textVisible.value = true
}

async function removeKey(key: SSHKey) {
    try {
        await ElMessageBox.confirm(`确定删除密钥「${key.name}」？删除后不可恢复。`, '删除密钥', {
            confirmButtonText: '删除',
            cancelButtonText: '取消',
            type: 'warning',
        })
    } catch {
        return
    }
    try {
        await SSHKeyService.Delete(key.id)
        ElMessage.success('已删除')
        await loadKeys()
    } catch (e: any) {
        ElMessage.error(`删除失败：${e?.message || e}`)
    }
}

function openDeploy(key: SSHKey) {
    deployKey.value = key
    deployConnId.value = undefined
    deployVisible.value = true
}

async function doDeploy() {
    if (!deployKey.value || !deployConnId.value) return
    deploying.value = true
    try {
        const msg = await SSHKeyService.Deploy(deployKey.value.id, deployConnId.value)
        deployVisible.value = false
        ElMessage.success(msg || '部署成功')
    } catch (e: any) {
        ElMessage.error(`部署失败：${e?.message || e}`)
    } finally {
        deploying.value = false
    }
}

onMounted(async () => {
    await Promise.all([loadKeys(), loadConns()])
})
</script>

<style scoped>
.sk-page {
    max-width: 1080px;
}

.sk-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 10px;
}

.sk-tip {
    font-size: 12px;
    color: var(--text-secondary);
}

.sk-head-actions {
    display: flex;
    gap: 8px;
    flex-shrink: 0;
}

.sk-table {
    max-width: 1080px;
}

.sk-fp {
    font-size: 12px;
    cursor: pointer;
    word-break: break-all;
}

.sk-fp:hover {
    color: var(--active-text);
}

.mono {
    font-family: Cascadia Code, JetBrains Mono, Consolas, monospace;
}

.dim {
    color: var(--text-secondary);
}

.sk-alert {
    margin-bottom: 10px;
}

.sk-deploy-key {
    font-size: 12px;
    color: var(--text-secondary);
    margin-bottom: 12px;
    word-break: break-all;
}

.sk-deploy-fp {
    color: var(--text-primary);
}

.sk-deploy-note {
    font-size: 12px;
    color: var(--text-muted);
    line-height: 1.7;
}

.sk-row {
    display: flex;
    gap: 8px;
    width: 100%;
}

.sk-row .el-input {
    flex: 1;
}

.sk-import-note {
    font-size: 12px;
    color: var(--text-muted);
    line-height: 1.7;
    margin-top: -6px;
}

.sk-batch-list {
    margin-top: 10px;
    max-height: 220px;
    overflow: auto;
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 4px 8px;
}

.sk-batch-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 4px 0;
    font-size: 12.5px;
}

.sk-batch-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.sk-batch-wait {
    flex-shrink: 0;
}

.sk-batch-fail {
    margin-top: 8px;
    max-height: 120px;
    overflow: auto;
    font-size: 12px;
    color: var(--danger-color);
    line-height: 1.6;
}

.sk-batch-progress {
    margin-top: 8px;
    font-size: 12px;
    color: var(--text-secondary);
}
</style>