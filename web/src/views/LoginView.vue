<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowRight, Eye, EyeOff, KeyRound, Loader2, ShieldCheck, Terminal } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'
import { api, ApiError } from '@/api/client'
import type { PublicConfig } from '@/api/types'

const auth = useAuthStore()
const toast = useToastStore()
const route = useRoute()
const router = useRouter()
const adminID = ref(localStorage.getItem('tenon_ops_admin_id') || '')
const password = ref('')
const showPassword = ref(false)
const busy = ref(false)
const error = ref('')
const retryAfter = ref(0)
const config = ref<PublicConfig | null>(null)
let timer: number | undefined

const canSubmit = computed(() => adminID.value.trim() !== '' && password.value !== '' && !busy.value && retryAfter.value === 0)
const lockText = computed(() => {
  if (retryAfter.value <= 0) return ''
  const m = Math.floor(retryAfter.value / 60)
  const s = retryAfter.value % 60
  return `尝试次数过多，请 ${m > 0 ? `${m} 分 ` : ''}${s} 秒后重试`
})

onMounted(async () => {
  try { config.value = await api.publicConfig() } catch { /* optional */ }
  const me = await auth.ensure()
  if (me) await redirect()
})

function safeRedirect(): string {
  const value = typeof route.query.redirect === 'string' ? route.query.redirect : ''
  if (value.startsWith('/') && !value.startsWith('//')) return value
  return '/'
}

async function redirect() {
  await router.replace(safeRedirect())
}

async function submit() {
  if (!canSubmit.value) return
  busy.value = true
  error.value = ''
  try {
    await auth.login(adminID.value.trim(), password.value)
    localStorage.setItem('tenon_ops_admin_id', adminID.value.trim())
    toast.success('登录成功')
    await redirect()
  } catch (e) {
    if (e instanceof ApiError) {
      if (e.status === 429) {
        retryAfter.value = Math.max(1, e.retryAfter || 60)
        startCountdown()
      } else {
        error.value = e.message
      }
    } else {
      error.value = '登录失败，请稍后重试'
    }
  } finally {
    busy.value = false
  }
}

function startCountdown() {
  if (timer) window.clearInterval(timer)
  timer = window.setInterval(() => {
    retryAfter.value--
    if (retryAfter.value <= 0) {
      window.clearInterval(timer)
      timer = undefined
    }
  }, 1000)
}
</script>

<template>
  <div class="login-page">
    <section class="login-art">
      <div class="login-brand">
        <div class="brand-mark">T</div>
        <div>
          <div style="font-weight:700;letter-spacing:.2px">Tenon Ops</div>
          <div style="font-size:10px;color:#64748b;letter-spacing:.14em;text-transform:uppercase">operations console</div>
        </div>
      </div>

      <div>
        <div style="display:inline-flex;align-items:center;gap:7px;border:1px solid rgba(94,234,212,.25);background:rgba(20,184,166,.08);color:#5eead4;border-radius:999px;padding:5px 10px;font-size:11px;margin-bottom:18px">
          <span class="env-dot" style="background:#2dd4bf" />{{ config?.environment || 'tenon_prod' }} · {{ config?.mode || 'remote' }}
        </div>
        <h1 class="login-title">把 Tenon 的<br /><span>身份、成本与轨迹</span><br />放在一个控制台里。</h1>
        <p class="login-copy">
          管理员登录、Tenon 用户开通、模型 token 与成本、当前活跃用户、Agent 执行轨迹——本地密码明文不在数据库中，所有敏感操作留审计。
        </p>
      </div>

      <div class="login-points">
        <div class="login-point"><ShieldCheck :size="15" /> 服务器 pepper + 数据库 verifier，DB 泄露不能登录</div>
        <div class="login-point"><KeyRound :size="15" /> 查看用户密码需要重新认证与原因</div>
        <div class="login-point"><Terminal :size="15" /> 管理端只监听 loopback，HTTPS 由反向代理终止</div>
      </div>
    </section>

    <section class="login-panel">
      <div class="login-card">
        <h2>管理员登录</h2>
        <p>使用 `tenon-ops init-admin` 创建的管理员身份。</p>

        <form class="stack" @submit.prevent="submit">
          <div class="field">
            <label class="label" for="admin-id">管理员 ID</label>
            <input id="admin-id" v-model="adminID" class="input" autocomplete="username" placeholder="例如 zhangyong" :disabled="busy" />
          </div>
          <div class="field">
            <label class="label" for="password">密码</label>
            <div style="position:relative">
              <input id="password" v-model="password" class="input" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" placeholder="输入管理员密码" :disabled="busy" style="padding-right:40px" />
              <button type="button" class="btn btn-ghost btn-icon" style="position:absolute;right:2px;top:2px" :aria-label="showPassword ? '隐藏密码' : '显示密码'" @click="showPassword = !showPassword">
                <EyeOff v-if="showPassword" :size="16" /><Eye v-else :size="16" />
              </button>
            </div>
          </div>

          <div v-if="error" class="banner danger">{{ error }}</div>
          <div v-if="lockText" class="banner">{{ lockText }}</div>

          <button class="btn btn-primary btn-block" type="submit" :disabled="!canSubmit" style="height:40px">
            <Loader2 v-if="busy" :size="16" class="spin" />
            <span>{{ busy ? '登录中…' : '登录控制台' }}</span>
            <ArrowRight v-if="!busy" :size="16" />
          </button>
        </form>

        <div class="login-meta">
          忘记密码？在服务器执行 <span class="mono">tenon-ops recover-admin -admin-id &lt;id&gt;</span>，再更新 Mac 上的 <span class="mono">~/.tenon-ops/admin.secret</span>。
          <br />本页面不提供“找回密码”邮件，避免把恢复入口暴露给攻击者。
        </div>
      </div>
    </section>
  </div>
</template>
