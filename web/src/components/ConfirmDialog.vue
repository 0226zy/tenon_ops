<script setup lang="ts">
import Modal from './Modal.vue'
defineProps<{ open: boolean; title: string; text?: string; confirmText?: string; danger?: boolean; busy?: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'confirm'): void }>()
</script>

<template>
  <Modal :open="open" :title="title" narrow @close="emit('close')">
    <p style="margin:0;color:var(--muted);font-size:13px;line-height:1.65">{{ text }}</p>
    <template #footer>
      <button class="btn" :disabled="busy" @click="emit('close')">取消</button>
      <button class="btn" :class="danger ? 'btn-danger' : 'btn-primary'" :disabled="busy" @click="emit('confirm')">
        {{ busy ? '处理中…' : (confirmText || '确认') }}
      </button>
    </template>
  </Modal>
</template>
