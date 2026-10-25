<script setup lang="ts">
import { useToastStore } from '@/stores/toast'
import { CheckCircle2, AlertTriangle, Info, X } from 'lucide-vue-next'
const toasts = useToastStore()
</script>

<template>
  <div class="toasts" aria-live="polite">
    <TransitionGroup name="toast">
      <div v-for="t in toasts.items" :key="t.id" class="toast" :class="t.kind">
        <CheckCircle2 v-if="t.kind === 'success'" :size="16" />
        <AlertTriangle v-else-if="t.kind === 'error'" :size="16" />
        <Info v-else :size="16" />
        <span>{{ t.text }}</span>
        <button aria-label="关闭" @click="toasts.remove(t.id)"><X :size="14" /></button>
      </div>
    </TransitionGroup>
  </div>
</template>
