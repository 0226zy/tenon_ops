<script setup lang="ts">
import { ref } from 'vue'
import { Check, Copy } from 'lucide-vue-next'

const props = defineProps<{ text: string; label?: string; tone?: 'dark' | 'light' }>()
const copied = ref(false)
async function copy() {
  try {
    await navigator.clipboard.writeText(props.text)
  } catch {
    const el = document.createElement('textarea')
    el.value = props.text
    document.body.appendChild(el)
    el.select()
    document.execCommand('copy')
    el.remove()
  }
  copied.value = true
  window.setTimeout(() => { copied.value = false }, 1500)
}
</script>

<template>
  <button class="btn btn-sm" :class="tone === 'dark' ? 'copy-btn' : ''" @click.stop="copy">
    <Check v-if="copied" :size="13" /><Copy v-else :size="13" />
    <span>{{ copied ? '已复制' : (label || '复制') }}</span>
  </button>
</template>
