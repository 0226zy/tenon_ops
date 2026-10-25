<script setup lang="ts">
withDefaults(defineProps<{ label: string; value: string | number; meta?: string; tone?: 'default' | 'accent' | 'danger' | 'warning' }>(), { tone: 'default' })
</script>

<template>
  <div class="card stat">
    <div class="stat-label"><slot name="icon" />{{ label }}</div>
    <div class="stat-value" :class="`stat-${tone}`">{{ value }}</div>
    <div v-if="meta || $slots.meta" class="stat-meta"><slot name="meta">{{ meta }}</slot></div>
  </div>
</template>
