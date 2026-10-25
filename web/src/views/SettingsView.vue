<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  AlertTriangle, CheckCircle2, KeyRound, Loader2, RefreshCw, Save, Server, ShieldCheck, XCircle,
} from 'lucide-vue-next'
import PageHeader from '@/components/PageHeader.vue'
import Badge from '@/components/Badge.vue'
import { api, ApiError } from '@/api/client'
import type { Health, Setting } from '@/api/types'
import { fmtTime } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'

const auth = useAuthStore()
const toast = useToastStore()
const router = useRouter()
const health = ref<Health | null>(null)
const settings = ref<Setting[]>([])
const loading = ref(true)
const error = ref('')
const saving = ref(false)

const form = reactive<Record<string, string>>({
  'ui.timezone': 'Asia/Shanghai', 'ui.refresh_default_seconds': '30', 'presence.default_window': '15m',
  'usage.cache_seconds': '60', 'audit.retention_days': '365', 'export.retention_hours': '24', 'export.max_rows': '50000',
})

const password = reactive({ old: '', new: '', confirm: '' })
const passwordBusy = ref(false)
const passwordError = ref('')

const checks = computed(() => health.value?.checks || [])
const okCount = computed(() => checks.value.filter((c) => c.status === 'ok').length)
const errorCount = computed(() => checks.value.filter((c) => c.status === 'error').length)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [h, s] = await Promise.all([api.system.health(), api.system.settings()])
    health.value = h
    settings.value = s.items
    for (const item of s.items) form[item.key] = item.value
  } catch (e) { error.value = e instanceof ApiError ? e.message : '加载系统信息失败' }
  finally { loading.value = false }
}

async function save() {
  saving.value = true
  try {
    const payload: Record<string, string> = {}
    for (const k of Object.keys(form)) payload[k] = String(form[k])
    await api.system.updateSettings(payload)
    toast.success('设置已保存')
    await load()
  } catch (e) { toast.error(e instanceof ApiError ? e.message : '保存失败') }
  finally { saving.value = false }
}

async function changePassword() {
  passwordError.value = ''
  if (password.new.length < 12) { passwordError.value = '新密码至少 12 个字节'; return }
  if (password.new !== password.confirm) { passwordError.value = '两次输入的新密码不一致'; return }
  passwordBusy.value = true
  try {
    await api.auth.changePassword(password.old, password.new)
    auth.clear()
    toast.success('密码已修改，请重新登录')
    await router.push('/login')
  } catch (e) { passwordError.value = e instanceof ApiError ? e.message : '修改失败' }
  finally { passwordBusy.value = false }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="设置" subtitle="数据源、schema、密钥与运行参数。部分偏好为预留；实际生效值以服务器 config.yaml 为准。">
      <template #actions><button class="btn" :disabled="loading" @click="load"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button></template>
    </PageHeader>

    <div v-if="error" class="banner danger" style="margin-bottom:14px"><AlertTriangle :size="15" /> {{ error }}<button class="link-btn" style="margin-left:auto" @click="load">重试</button></div>
    <div v-if="loading && !health" class="card card-pad"><Loader2 :size="20" class="spin" /><span style="margin-left:8px">检查系统状态…</span></div>

    <template v-else-if="health">
      <div class="grid-4" style="margin-bottom:16px">
        <div class="card stat"><div class="stat-label"><Server :size="13" /> 环境</div><div class="stat-value" style="font-size:18px">{{ health.environment }}</div><div class="stat-meta">{{ health.mode }} · {{ health.exposure }}</div></div>
        <div class="card stat"><div class="stat-label"><ShieldCheck :size="13" /> 健康检查</div><div class="stat-value" :style="health.ok ? 'color:var(--success)' : 'color:var(--danger)'">{{ health.ok ? '正常' : '异常' }}</div><div class="stat-meta">{{ okCount }} ok · {{ errorCount }} error</div></div>
        <div class="card stat"><div class="stat-label">Tenon schema</div><div class="stat-value mono" style="font-size:16px">{{ health.schema_version || '未知' }}</div><div class="stat-meta">{{ health.tenon_schema }}</div></div>
        <div class="card stat"><div class="stat-label">管理员</div><div class="stat-value">{{ health.admin_count }}</div><div class="stat-meta">v{{ health.version }} · {{ fmtTime(health.time) }}</div></div>
      </div>

      <div class="grid-2" style="align-items:start">
        <section class="card">
          <div class="card-head"><h2 class="card-title">健康检查</h2><span class="small muted" style="margin-left:auto">服务端自检</span></div>
          <div class="card-body">
            <div class="hr-list">
              <div v-for="c in checks" :key="c.name" class="hr-item">
                <CheckCircle2 v-if="c.status === 'ok'" :size="16" style="color:var(--success);flex:none" />
                <AlertTriangle v-else-if="c.status === 'warn'" :size="16" style="color:var(--warning);flex:none" />
                <XCircle v-else :size="16" style="color:var(--danger);flex:none" />
                <div style="min-width:0;flex:1"><div class="mono small" style="font-weight:600">{{ c.name }}</div><div class="small muted" style="word-break:break-word">{{ c.message }}</div></div>
                <Badge :tone="c.status === 'ok' ? 'green' : c.status === 'warn' ? 'amber' : 'red'">{{ c.status }}</Badge>
              </div>
            </div>
          </div>
        </section>

        <section class="card">
          <div class="card-head"><h2 class="card-title">运行参数</h2><button class="btn btn-primary btn-sm" style="margin-left:auto" :disabled="saving" @click="save"><Save :size="13" /> 保存</button></div>
          <div class="card-body form-grid">
            <div class="form-row"><div class="field"><label class="label">展示时区</label><input v-model="form['ui.timezone']" class="input" /></div><div class="field"><label class="label">默认刷新秒数</label><input v-model="form['ui.refresh_default_seconds']" class="input" type="number" /></div></div>
            <div class="form-row"><div class="field"><label class="label">默认活跃窗口</label><select v-model="form['presence.default_window']" class="select"><option>5m</option><option>15m</option><option>60m</option><option>24h</option></select></div><div class="field"><label class="label">用量缓存秒数</label><input v-model="form['usage.cache_seconds']" class="input" type="number" /></div></div>
            <div class="form-row"><div class="field"><label class="label">审计保留天数</label><input v-model="form['audit.retention_days']" class="input" type="number" /></div><div class="field"><label class="label">导出保留小时</label><input v-model="form['export.retention_hours']" class="input" type="number" /></div></div>
            <div class="field"><label class="label">单次导出行数上限</label><input v-model="form['export.max_rows']" class="input" type="number" /></div>
            <div class="hint">设置保存到 <span class="mono">tadm_setting</span> 并写审计。当前版本中时区/刷新/保留期部分为预留或后续任务参数；实际生效值以服务器 <span class="mono">config.yaml</span> 为准。</div>
          </div>
        </section>
      </div>

      <div class="grid-2" style="margin-top:16px;align-items:start">
        <section class="card">
          <div class="card-head"><h2 class="card-title">管理员密码</h2><KeyRound :size="15" style="margin-left:auto;color:var(--muted)" /></div>
          <div class="card-body form-grid">
            <div class="field"><label class="label">当前密码</label><input v-model="password.old" class="input" type="password" autocomplete="current-password" /></div>
            <div class="form-row"><div class="field"><label class="label">新密码</label><input v-model="password.new" class="input" type="password" autocomplete="new-password" /><div class="hint">至少 12 个字节。</div></div><div class="field"><label class="label">确认新密码</label><input v-model="password.confirm" class="input" type="password" autocomplete="new-password" /></div></div>
            <div v-if="passwordError" class="error-text">{{ passwordError }}</div>
            <button class="btn" :disabled="passwordBusy" @click="changePassword"><Loader2 v-if="passwordBusy" :size="14" class="spin" /> 修改密码并退出所有会话</button>
          </div>
        </section>
        <section class="card">
          <div class="card-head"><h2 class="card-title">密钥与部署</h2><ShieldCheck :size="15" style="margin-left:auto;color:var(--muted)" /></div>
          <div class="card-body stack small muted">
            <div class="row"><span class="mono">master.key</span><span class="spacer" /><Badge :tone="checks.find((c) => c.name === 'master_key')?.status === 'ok' ? 'green' : 'amber'">{{ checks.find((c) => c.name === 'master_key')?.message || '未知' }}</Badge></div>
            <p style="margin:0">服务器持有 <span class="mono">master.key</span>（0600 / systemd credential）；管理员密码明文只在 Mac 的 <span class="mono">admin.secret</span>。数据库只存 verifier 与密文。</p>
            <p style="margin:0">公网入口必须启用 WebAuthn/MFA 或置于 VPN 后；本版本 public 模式默认拒绝启动。</p>
            <div class="divider" />
            <div class="row"><span>监听</span><span class="spacer" /><span class="mono">{{ health.listen }}</span></div>
            <div class="row"><span>Public URL</span><span class="spacer" /><span class="mono">{{ health.public_url || '—' }}</span></div>
            <div class="row"><span>最近用量</span><span class="spacer" /><span>{{ fmtTime(health.last_usage_at) }}</span></div>
            <div class="row"><span>最近事件</span><span class="spacer" /><span>{{ fmtTime(health.last_event_at) }}</span></div>
          </div>
        </section>
      </div>
    </template>
  </div>
</template>
