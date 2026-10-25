<script setup lang="ts">
import { Inbox } from 'lucide-vue-next'
defineProps<{ title: string; text?: string }>()
</script>

<template>
  <div class="empty">
    <slot name="icon"><Inbox :size="28" /></slot>
    <div class="empty-title">{{ title }}</div>
    <div v-if="text" class="small">{{ text }}</div>
    <div v-if="$slots.action" style="margin-top:14px"><slot name="action" /></div>
  </div>
</template>
