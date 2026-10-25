<script setup lang="ts">
withDefaults(defineProps<{ tone?: string; dot?: boolean }>(), { tone: 'gray', dot: false })
</script>

<template>
  <span class="badge" :class="`badge-${tone}`"><span v-if="dot" class="dot" /><slot /></span>
</template>
