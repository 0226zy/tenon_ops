<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { KeyRound } from 'lucide-vue-next'
import Modal from './Modal.vue'
import { useAuthStore } from '@/stores/auth'
import { ApiError } from '@/api/client'

const props = defineProps<{ open: boolean; title?: string; subtitle?: string }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'done'): void }>()
const auth = useAuthStore()
const password = ref('')
const error = ref('')
const busy = ref(false)
const input = ref<HTMLInputElement | null>(null)

watch(() => props.open, async (open) => {
  if (open) {
    password.value = ''
    error.value = ''
    await nextTick()
    input.value?.focus()
  }
})

async function submit() {
  if (busy.value || !password.value) return
  busy.value = true
  error.value = ''
  try {
    await auth.stepUp(password.value)
    emit('done')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '验证失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Modal :open="open" :title="title || '确认管理员密码'" narrow @close="emit('close')">
    <div class="stack">
      <div class="row" style="align-items:flex-start;gap:10px">
        <span style="width:34px;height:34px;border-radius:9px;background:var(--accent-soft);color:var(--accent);display:grid;place-items:center;flex:none"><KeyRound :size="17" /></span>
        <div>
          <div style="font-weight:600;font-size:13px">敏感操作需要重新认证</div>
          <div class="hint">{{ subtitle || '验证成功后有 5 分钟操作窗口。' }}</div>
        </div>
      </div>
      <form class="field" @submit.prevent="submit">
        <label class="label">管理员密码</label>
        <input ref="input" v-model="password" class="input" type="password" autocomplete="current-password" placeholder="输入当前管理员密码" />
        <div v-if="error" class="error-text">{{ error }}</div>
        <button class="btn btn-primary btn-block" :disabled="busy || !password" type="submit">{{ busy ? '验证中…' : '验证并继续' }}</button>
      </form>
    </div>
  </Modal>
</template>
