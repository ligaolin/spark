<template>
    <el-dialog :model-value="modelValue" :title="'设置环境 - ' + (node?.name || '')" width="750px" destroy-on-close
        @update:model-value="onVisibleChange">
        <!-- 基础链接区域 -->
        <el-divider content-position="left">
            <el-icon>
                <Connection />
            </el-icon>
            <span style="margin-left:4px">基础链接</span>
        </el-divider>
        <p class="env-tab-hint">
            <el-icon>
                <InfoFilled />
            </el-icon>
            可配置多个基础链接（如正式/测试），标记一个为「当前使用」。为空则继承上级目录。
        </p>
        <div class="env-base-url-list">
            <div class="env-base-item" v-for="(item, i) in folderEnvList" :key="item.id || ('new_' + i)">
                <el-input v-model="item.name" class="env-base-name" placeholder="名称（如：正式、测试）" size="small" />
                <el-input v-model="item.baseUrl" class="env-base-url" placeholder="https://api.example.com" size="small" />
                <el-tag v-if="item.isActive" size="small" type="success" class="env-active-tag">当前使用</el-tag>
                <el-button v-else size="small" text type="primary" @click="setActiveEnvItem(item)">启用</el-button>
                <el-button size="small" text type="danger" :icon="Delete" @click="removeEnvItem(i)" />
            </div>
            <el-button size="small" :icon="Plus" @click="addEnvItem">添加基础链接</el-button>
        </div>

        <!-- 公共请求头区域 -->
        <el-divider content-position="left" style="margin-top:20px">
            <el-icon>
                <Setting />
            </el-icon>
            <span style="margin-left:4px">公共请求头</span>
        </el-divider>
        <p class="env-tab-hint">
            <el-icon>
                <InfoFilled />
            </el-icon>
            文件夹级公共请求头，所有子请求自动携带。优先级：请求自身 Headers > 文件夹公共请求头。同 key 由高优先级覆盖。
        </p>
        <div class="kv-editor" style="height: auto; max-height: 300px; border: 1px solid var(--border-color); border-radius: 6px;">
            <div class="kv-row" v-for="(row, i) in folderHeadersForm" :key="i">
                <el-input v-model="row.key" class="kv-key" placeholder="Header 名" size="small" />
                <el-input v-model="row.value" class="kv-val" placeholder="Header 值" size="small" />
                <el-button size="small" :icon="Delete" circle @click="folderHeadersForm.splice(i, 1)" />
            </div>
            <el-button size="small" :icon="Plus"
                @click="folderHeadersForm.push({ key: '', value: '', enabled: true })">
                添加请求头
            </el-button>
        </div>
        <template #footer>
            <el-button @click="emit('update:modelValue', false)">取消</el-button>
            <el-button type="primary" @click="save" :loading="saving">保存</el-button>
        </template>
    </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Connection, Delete, InfoFilled, Plus, Setting } from '@element-plus/icons-vue'
import * as RestService from '../../../bindings/spark/app/service/rest/restservice.js'
import type { KV } from '../../utils/restCurl'

interface FolderEnvItem {
    id: number
    folderId: number
    name: string
    baseUrl: string
    isActive: boolean
    sort: number
}

const props = defineProps<{
    modelValue: boolean
    node: { id: number; name: string; parentId: number; commonHeaders?: string } | null
}>()

const emit = defineEmits<{
    (e: 'update:modelValue', v: boolean): void
    (e: 'saved', node: { id: number; name: string; parentId: number; commonHeaders?: string }): void
}>()

const folderEnvList = ref<FolderEnvItem[]>([])
const folderHeadersForm = ref<KV[]>([])
const saving = ref(false)

function onVisibleChange(v: boolean) {
    emit('update:modelValue', v)
}

watch(
    () => props.modelValue,
    async (v) => {
        if (!v || !props.node) return
        const node = props.node

        // 加载基础链接列表
        try {
            const list = await RestService.ListFolderEnvs(node.id)
            folderEnvList.value = (list ?? []) as FolderEnvItem[]
        } catch {
            folderEnvList.value = []
        }

        // 加载公共请求头
        try {
            if (node.commonHeaders) {
                folderHeadersForm.value = JSON.parse(node.commonHeaders).map((h: KV) => ({ enabled: true, ...h }))
            } else {
                folderHeadersForm.value = []
            }
        } catch {
            folderHeadersForm.value = []
        }
    },
)

function addEnvItem() {
    folderEnvList.value.push({
        id: 0,
        folderId: props.node?.id ?? 0,
        name: '',
        baseUrl: '',
        isActive: folderEnvList.value.length === 0,
        sort: folderEnvList.value.length,
    })
}

function removeEnvItem(index: number) {
    folderEnvList.value.splice(index, 1)
}

async function setActiveEnvItem(item: FolderEnvItem) {
    if (item.id) {
        try {
            await RestService.SetActiveFolderEnv(item.id)
            for (const e of folderEnvList.value) {
                e.isActive = e.id === item.id
            }
        } catch (e: any) {
            ElMessage.error('设置失败: ' + (e?.message || e))
        }
    } else {
        // 新条目，先标记
        for (const e of folderEnvList.value) {
            e.isActive = e === item
        }
    }
}

async function save() {
    const target = props.node
    if (!target) return

    saving.value = true
    try {
        // 1. 保存所有基础链接
        for (const item of folderEnvList.value) {
            await RestService.SaveFolderEnv({
                id: item.id,
                folderId: target.id,
                name: item.name.trim(),
                baseUrl: item.baseUrl.trim(),
                isActive: item.isActive,
            })
        }
        // 删除已移除的基础链接（需要后台支持，这里先跳过）

        // 2. 保存公共请求头
        const validHeaders = folderHeadersForm.value.filter((h) => h.key.trim())
        await RestService.SetFolderCommonHeaders(target.id, JSON.stringify(validHeaders))

        emit('update:modelValue', false)
        emit('saved', target)
        ElMessage.success('环境设置已保存')
    } catch (e: any) {
        ElMessage.error('保存失败: ' + (e?.message || e))
    } finally {
        saving.value = false
    }
}
</script>

<style scoped>
.env-tab-hint {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--text-tertiary);
    margin-bottom: 12px;
    padding: 6px 10px;
    background: var(--bg-tertiary);
    border-radius: 4px;
}

.env-base-url-list {
    max-height: 450px;
    overflow-y: auto;
}

.env-base-item {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 8px;
    padding: 6px 8px;
    background: var(--bg-secondary);
    border-radius: 6px;
}

.env-base-name {
    width: 120px;
    flex-shrink: 0;
}

.env-base-url {
    flex: 1;
}

.env-active-tag {
    flex-shrink: 0;
}

/* 公共请求头区域复用的键值行样式（与请求编辑器保持一致） */
.kv-editor {
    padding: 12px 16px;
    overflow-y: auto;
    height: 100%;
    box-sizing: border-box;
}

.kv-row {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 6px;
}

.kv-key {
    width: 200px;
    flex-shrink: 0;
}

.kv-val {
    flex: 1;
}
</style>