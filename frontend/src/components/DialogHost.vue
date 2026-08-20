<template>
  <!-- 全局页面内弹窗宿主：InputDialog / ConfirmDialog 由 utils/dialog 驱动 -->
  <InputDialog
    v-model="inputState.visible"
    :title="inputState.title"
    :fields="inputState.fields"
    @confirm="onInputConfirm"
    @cancel="onInputCancel"
  />
  <ConfirmDialog
    v-model="confirmState.visible"
    :title="confirmState.title"
    :message="confirmState.message"
    :danger="confirmState.danger"
    :confirm-text="confirmState.confirmText"
    :show-cancel="confirmState.showCancel"
    @confirm="onConfirmOk"
    @cancel="onConfirmCancel"
  />
</template>

<script setup lang="ts">
import InputDialog from './InputDialog.vue'
import ConfirmDialog from './ConfirmDialog.vue'
import { inputState, confirmState } from '../utils/dialog'

function onInputConfirm(values: Record<string, string>) {
  inputState.visible = false
  inputState.resolve?.(values)
  inputState.resolve = null
}

function onInputCancel() {
  inputState.visible = false
  inputState.resolve?.(null)
  inputState.resolve = null
}

function onConfirmOk() {
  confirmState.visible = false
  confirmState.resolve?.(true)
  confirmState.resolve = null
}

function onConfirmCancel() {
  confirmState.visible = false
  confirmState.resolve?.(false)
  confirmState.resolve = null
}
</script>
