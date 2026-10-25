import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastKind = 'success' | 'error' | 'info'
export interface Toast { id: number; kind: ToastKind; text: string }

export const useToastStore = defineStore('toast', () => {
  const items = ref<Toast[]>([])
  let next = 1
  function push(kind: ToastKind, text: string): void {
    const id = next++
    items.value.push({ id, kind, text })
    window.setTimeout(() => remove(id), kind === 'error' ? 6500 : 4000)
  }
  function remove(id: number): void {
    items.value = items.value.filter((v) => v.id !== id)
  }
  return {
    items,
    remove,
    success: (text: string) => push('success', text),
    error: (text: string) => push('error', text),
    info: (text: string) => push('info', text),
  }
})
