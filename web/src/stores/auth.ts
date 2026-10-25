import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api, setCSRF } from '@/api/client'
import type { Me } from '@/api/types'

export const useAuthStore = defineStore('auth', () => {
  const me = ref<Me | null>(null)
  const checked = ref(false)
  const loading = ref(false)
  let ensurePromise: Promise<Me | null> | null = null

  const authenticated = computed(() => !!me.value)
  const elevated = computed(() => !!me.value?.elevated)
  const environment = computed(() => me.value?.environment || 'tenon_prod')
  const mode = computed(() => me.value?.mode || 'remote')
  const version = computed(() => me.value?.version || '0.1.0')

  async function ensure(force = false): Promise<Me | null> {
    if (checked.value && !force) return me.value
    if (ensurePromise) return ensurePromise
    loading.value = true
    ensurePromise = api.auth.me()
      .then((value) => {
        me.value = value
        setCSRF(value.csrf_token)
        checked.value = true
        return value
      })
      .catch(() => {
        me.value = null
        checked.value = true
        return null
      })
      .finally(() => { loading.value = false; ensurePromise = null })
    return ensurePromise
  }

  async function login(adminID: string, password: string): Promise<void> {
    await api.auth.login(adminID, password)
    await ensure(true)
  }

  async function logout(): Promise<void> {
    try { await api.auth.logout() } catch { /* ignore */ }
    clear()
  }

  async function stepUp(password: string): Promise<void> {
    await api.auth.stepUp(password)
    await ensure(true)
  }

  function clear(): void {
    me.value = null
    checked.value = true
    setCSRF('')
  }

  return { me, checked, loading, authenticated, elevated, environment, mode, version, ensure, login, logout, stepUp, clear }
})
