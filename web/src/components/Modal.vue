<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { X } from 'lucide-vue-next'

const props = withDefaults(defineProps<{ open: boolean; title: string; wide?: boolean; narrow?: boolean; closable?: boolean }>(), { wide: false, narrow: false, closable: true })
const emit = defineEmits<{ (e: 'close'): void }>()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open && props.closable) emit('close')
}
watch(() => props.open, (open) => {
  if (open) {
    document.addEventListener('keydown', onKey)
    document.body.style.overflow = 'hidden'
  } else {
    document.removeEventListener('keydown', onKey)
    document.body.style.overflow = ''
  }
}, { immediate: true })
onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKey)
  document.body.style.overflow = ''
})
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="modal-backdrop" @mousedown.self="closable && emit('close')">
      <div class="modal" :class="{ wide, 'modal-narrow': narrow }" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h3 class="modal-title">{{ title }}</h3>
          <button v-if="closable" class="close" aria-label="关闭" @click="emit('close')"><X :size="17" /></button>
        </div>
        <div class="modal-body"><slot /></div>
        <div v-if="$slots.footer" class="modal-foot"><slot name="footer" /></div>
      </div>
    </div>
  </Teleport>
</template>
