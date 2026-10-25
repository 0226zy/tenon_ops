<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import {
  AlertTriangle, Check, Eye, KeyRound, Loader2, Plus, RefreshCw, Search, ShieldOff,
  ShieldCheck, UserRound, Users, X,
} from 'lucide-vue-next'
import PageHeader from '@/components/PageHeader.vue'
import Badge from '@/components/Badge.vue'
import Modal from '@/components/Modal.vue'
import StepUpModal from '@/components/StepUpModal.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import CopyButton from '@/components/CopyButton.vue'
import EmptyState from '@/components/EmptyState.vue'
import { api, ApiError } from '@/api/client'
import type { CreateUserResult, ResetResult, User } from '@/api/types'
import { fmtInt, fmtNumber, pwdLabel, pwdTone, timeAgo, userTone } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'

const auth = useAuthStore()
const toast = useToastStore()
const items = ref<User[]>([])
const next = ref('')
const loading = ref(true)
const loadingMore = ref(false)
const error = ref('')
const query = ref('')
const status = ref('')
const pwdState = ref('')

const createOpen = ref(false)
const createBusy = ref(false)
const createError = ref('')
const created = ref<CreateUserResult | null>(null)
const form = reactive({ user_id: '', name: '', password: '', max_conv: 20, tags: '', note: '' })

const revealTarget = ref<User | null>(null)
const revealOpen = ref(false)
const revealBusy = ref(false)
const revealError = ref('')
const revealReason = ref('')
const revealed = ref<{ user_id: string; password: string; expires_in: number } | null>(null)
const stepUpOpen = ref(false)
const stepUpReason = ref('')
let revealTimer: number | undefined

const resetTarget = ref<User | null>(null)
const resetOpen = ref(false)
const resetBusy = ref(false)
const resetError = ref('')
const resetReason = ref('')
const resetPassword = ref('')
const resetResult = ref<ResetResult | null>(null)

const confirmTarget = ref<User | null>(null)
const confirmKind = ref<'disable' | 'enable' | 'revoke'>('disable')
const confirmOpen = ref(false)
const confirmBusy = ref(false)

const hasMore = computed(() => !!next.value)

async function load(reset = true) {
  if (reset) loading.value = true
  else loadingMore.value = true
  error.value = ''
  try {
    const page = await api.users.list({
      q: query.value.trim(), status: status.value ? Number(status.value) : undefined,
      pwd_state: pwdState.value || undefined, cursor: reset ? undefined : next.value, limit: 50,
    })
    items.value = reset ? page.items : [...items.value, ...page.items]
    next.value = page.next_cursor
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载用户失败'
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

function resetFilters() {
  query.value = ''
  status.value = ''
  pwdState.value = ''
  void load()
}

function openCreate() {
  Object.assign(form, { user_id: '', name: '', password: '', max_conv: 20, tags: '', note: '' })
  createError.value = ''
  created.value = null
  createOpen.value = true
}

async function submitCreate() {
  if (createBusy.value) return
  if (!/^[A-Za-z0-9_.-]{1,64}$/.test(form.user_id.trim())) {
    createError.value = '用户 ID 只能包含字母、数字、点、下划线或短横线，长度 1–64。'
    return
  }
  createBusy.value = true
  createError.value = ''
  try {
    const res = await api.users.create({
      user_id: form.user_id.trim(), name: form.name.trim() || form.user_id.trim(),
      password: form.password || undefined, max_conv: Number(form.max_conv) || 20,
      tags: form.tags.split(',').map((v) => v.trim()).filter(Boolean),
      note: form.note.trim(),
    })
    created.value = res
    toast.success('Tenon 用户已创建')
    await load()
  } catch (e) {
    createError.value = e instanceof ApiError ? e.message : '创建失败'
  } finally {
    createBusy.value = false
  }
}

function startReveal(u: User) {
  revealTarget.value = u
  revealError.value = ''
  revealReason.value = ''
  revealed.value = null
  if (!auth.elevated) {
    stepUpReason.value = '查看 Tenon 用户密码需要重新认证。'
    stepUpOpen.value = true
  } else {
    revealOpen.value = true
  }
}

async function submitReveal() {
  if (!revealTarget.value || revealBusy.value) return
  if (revealReason.value.trim().length < 4) {
    revealError.value = '请填写至少 4 个字符的原因。'
    return
  }
  revealBusy.value = true
  revealError.value = ''
  try {
    revealed.value = await api.users.reveal(revealTarget.value.user_id, revealReason.value.trim())
    toast.success('密码已解密，30 秒后自动隐藏')
    if (revealTimer) window.clearTimeout(revealTimer)
    revealTimer = window.setTimeout(() => { revealed.value = null; revealOpen.value = false }, 30_000)
  } catch (e) {
    if (e instanceof ApiError && e.code === 'STEP_UP_REQUIRED') {
      revealOpen.value = false
      stepUpReason.value = '查看 Tenon 用户密码需要重新认证。'
      stepUpOpen.value = true
    } else {
      revealError.value = e instanceof ApiError ? e.message : '查看失败'
    }
  } finally {
    revealBusy.value = false
  }
}

function onStepUpDone() {
  stepUpOpen.value = false
  if (revealTarget.value) revealOpen.value = true
}

function closeReveal() {
  if (revealTimer) window.clearTimeout(revealTimer)
  revealTimer = undefined
  revealed.value = null
  revealOpen.value = false
}

function openReset(u: User) {
  resetTarget.value = u
  resetReason.value = ''
  resetPassword.value = ''
  resetResult.value = null
  resetError.value = ''
  resetOpen.value = true
}

async function submitReset() {
  if (!resetTarget.value || resetBusy.value) return
  resetBusy.value = true
  resetError.value = ''
  try {
    resetResult.value = await api.users.reset(resetTarget.value.user_id, {
      password: resetPassword.value || undefined,
      reason: resetReason.value.trim() || '管理员重置',
    })
    toast.success('密码已重置，旧密码与旧会话已失效')
    await load()
  } catch (e) {
    resetError.value = e instanceof ApiError ? e.message : '重置失败'
  } finally {
    resetBusy.value = false
  }
}

function askConfirm(u: User, kind: 'disable' | 'enable' | 'revoke') {
  confirmTarget.value = u
  confirmKind.value = kind
  confirmOpen.value = true
}

async function doConfirm(reason = '') {
  if (!confirmTarget.value || confirmBusy.value) return
  confirmBusy.value = true
  try {
    if (confirmKind.value === 'disable') await api.users.disable(confirmTarget.value.user_id, reason)
    else if (confirmKind.value === 'enable') await api.users.enable(confirmTarget.value.user_id)
    else await api.users.revokeSessions(confirmTarget.value.user_id)
    toast.success(confirmKind.value === 'disable' ? '用户已停用' : confirmKind.value === 'enable' ? '用户已启用' : '会话已吊销')
    confirmOpen.value = false
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : '操作失败')
  } finally {
    confirmBusy.value = false
  }
}

const confirmTitle = computed(() => {
  const u = confirmTarget.value?.user_id || ''
  if (confirmKind.value === 'disable') return `停用用户 ${u}`
  if (confirmKind.value === 'enable') return `启用用户 ${u}`
  return `吊销 ${u} 的全部会话`
})
const confirmText = computed(() => {
  if (confirmKind.value === 'disable') return '停用后 Tenon 登录立即失败，所有现有会话失效；正在运行的 Run 不会被终止。'
  if (confirmKind.value === 'enable') return '启用后用户可用原密码登录；如需强制换密，请随后重置密码。'
  return '该用户的所有浏览器会话将被提前过期，需要重新登录。'
})

onMounted(() => { void load() })
</script>

<template>
  <div>
    <PageHeader title="用户管理" subtitle="直接写入 Tenon 的 t_user；密码可还原但以密文落库，查看需要重新认证与原因。">
      <template #actions>
        <button class="btn" :disabled="loading" @click="load()"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button>
        <button class="btn btn-primary" @click="openCreate"><Plus :size="15" /> 新建用户</button>
      </template>
    </PageHeader>

    <div v-if="error" class="banner danger" style="margin-bottom:14px"><AlertTriangle :size="15" /> {{ error }}<button class="link-btn" style="margin-left:auto" @click="load()">重试</button></div>

    <section class="card">
      <div class="table-toolbar">
        <div class="search">
          <Search :size="15" />
          <input v-model="query" class="input" placeholder="搜索 user_id 或显示名" @keyup.enter="load()" />
        </div>
        <select v-model="status" class="select" style="width:auto" @change="load()">
          <option value="">全部状态</option><option value="1">正常</option><option value="2">已停用</option>
        </select>
        <select v-model="pwdState" class="select" style="width:auto" @change="load()">
          <option value="">全部密码状态</option><option value="managed">可查看</option><option value="unknown">历史未知</option><option value="delivered_removed">交付后清除</option>
        </select>
        <button class="btn" @click="load()">筛选</button>
        <button class="btn btn-ghost" @click="resetFilters">重置</button>
        <span class="spacer" />
        <span class="small muted">{{ items.length }} 个用户{{ hasMore ? '（还有更多）' : '' }}</span>
      </div>

      <div v-if="loading" class="table-empty"><Loader2 :size="22" class="spin" /><div>加载用户中…</div></div>
      <div v-else-if="!items.length" class="table-empty">
        <EmptyState title="没有匹配的用户" text="调整筛选条件，或创建一个新的 Tenon 用户。" />
      </div>
      <div v-else class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>用户</th><th>状态</th><th>密码</th><th class="right">会话</th><th class="right">30 天 Token</th>
              <th>最近活跃</th><th>标签</th><th style="width:210px"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in items" :key="u.user_id">
              <td>
                <RouterLink :to="`/users/${encodeURIComponent(u.user_id)}`" class="mono" style="font-weight:600">{{ u.user_id }}</RouterLink>
                <div class="small muted">{{ u.name }}<span v-if="u.max_conv"> · max_conv {{ u.max_conv }}</span></div>
              </td>
              <td><Badge :tone="userTone(u.status)" dot>{{ u.status === 1 ? '正常' : '已停用' }}</Badge></td>
              <td><Badge :tone="pwdTone(u.pwd_state)">{{ pwdLabel(u.pwd_state) }}</Badge></td>
              <td class="right"><span :class="{ 'stat-danger': u.active_runs > 0 }">{{ u.sessions }}</span><span v-if="u.active_runs" class="small muted"> / {{ u.active_runs }} run</span></td>
              <td class="right mono">{{ fmtNumber(u.tokens_30d) }}</td>
              <td><span class="small muted">{{ timeAgo(u.last_seen) }}</span></td>
              <td><span v-for="t in u.tags" :key="t" class="tag" style="margin-right:4px">{{ t }}</span></td>
              <td>
                <div class="actions">
                  <RouterLink class="btn btn-sm" :to="`/users/${encodeURIComponent(u.user_id)}`"><UserRound :size="13" /> 详情</RouterLink>
                  <button class="btn btn-sm" title="重置密码" @click="openReset(u)"><KeyRound :size="13" /></button>
                  <button class="btn btn-sm" title="查看密码" @click="startReveal(u)"><Eye :size="13" /></button>
                  <button v-if="u.status === 1" class="btn btn-sm" title="停用" @click="askConfirm(u, 'disable')"><ShieldOff :size="13" /></button>
                  <button v-else class="btn btn-sm" title="启用" @click="askConfirm(u, 'enable')"><ShieldCheck :size="13" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="hasMore" class="table-foot">
        <span>继续加载更早的用户</span>
        <button class="btn btn-sm" :disabled="loadingMore" @click="load(false)"><Loader2 v-if="loadingMore" :size="13" class="spin" /> 加载更多</button>
      </div>
    </section>

    <!-- Create -->
    <Modal :open="createOpen" :title="created ? '用户创建成功' : '新建 Tenon 用户'" :closable="!createBusy" @close="createOpen = false">
      <template v-if="!created">
        <div class="stack">
          <div class="banner info"><ShieldCheck :size="15" /><span>将直接写入 <span class="mono">tenon_prod.t_user</span>，Argon2id 哈希与 Tenon 登录兼容，创建后立即生效。</span></div>
          <div class="form-row">
            <div class="field"><label class="label">用户 ID</label><input v-model="form.user_id" class="input input-mono" placeholder="alice" /><div class="hint">字母、数字、点、下划线或短横线，1–64 位。</div></div>
            <div class="field"><label class="label">显示名</label><input v-model="form.name" class="input" placeholder="Alice" /></div>
          </div>
          <div class="form-row">
            <div class="field"><label class="label">初始密码</label><input v-model="form.password" class="input input-mono" placeholder="留空自动生成 24 位" /><div class="hint">8–128 字节；留空由服务端生成。</div></div>
            <div class="field"><label class="label">最大会话数</label><input v-model.number="form.max_conv" class="input" type="number" min="1" max="1000" /></div>
          </div>
          <div class="field"><label class="label">标签</label><input v-model="form.tags" class="input" placeholder="team-a, customer" /><div class="hint">逗号分隔，最多 10 个，仅管理端使用。</div></div>
          <div class="field"><label class="label">备注</label><textarea v-model="form.note" class="textarea" placeholder="交付说明、客户信息等（仅管理端可见）" maxlength="255" /></div>
          <div v-if="createError" class="error-text">{{ createError }}</div>
        </div>
      </template>
      <template v-else>
        <div class="stack">
          <div class="row"><span style="width:38px;height:38px;border-radius:50%;background:var(--success-soft);color:var(--success);display:grid;place-items:center"><Check :size="20" /></span><div><div style="font-weight:650">{{ created.user_id }}</div><div class="small muted">{{ created.name }} · max_conv {{ created.max_conv }}{{ created.replayed ? ' · 已存在（幂等重放）' : '' }}</div></div></div>
          <div v-if="created.password">
            <div class="label" style="margin-bottom:6px">初始密码（仅本次显示）</div>
            <div class="secret-box"><span style="flex:1">{{ created.password }}</span><CopyButton :text="created.password" tone="dark" /></div>
            <div class="hint" style="margin-top:6px">请立即交付并保存；离开后需要重新认证才能查看。</div>
          </div>
          <div v-else class="banner info">该请求已处理过；密码未在重放响应中返回，可在用户详情中“查看密码”。</div>
        </div>
      </template>
      <template #footer>
        <button v-if="!created" class="btn" :disabled="createBusy" @click="createOpen = false">取消</button>
        <button v-if="!created" class="btn btn-primary" :disabled="createBusy" @click="submitCreate"><Loader2 v-if="createBusy" :size="14" class="spin" /> 创建用户</button>
        <button v-else class="btn btn-primary" @click="createOpen = false">完成</button>
      </template>
    </Modal>

    <!-- Reveal -->
    <StepUpModal :open="stepUpOpen" :subtitle="stepUpReason" @close="stepUpOpen = false" @done="onStepUpDone" />
    <Modal :open="revealOpen" title="查看 Tenon 用户密码" :closable="!revealBusy" @close="closeReveal">
      <div class="stack">
        <div class="row"><div class="avatar" style="width:34px;height:34px">{{ revealTarget?.user_id.slice(0, 1).toUpperCase() }}</div><div><div style="font-weight:600">{{ revealTarget?.user_id }}</div><div class="small muted">{{ revealTarget?.name }}</div></div></div>
        <div v-if="!revealed">
          <div class="field"><label class="label">查看原因（必填，会写入审计）</label><input v-model="revealReason" class="input" placeholder="例如：交付给客户 A" maxlength="255" /></div>
          <div v-if="revealError" class="error-text" style="margin-top:8px">{{ revealError }}</div>
          <div class="banner" style="margin-top:12px">密码将在 30 秒后从页面清除；审计会记录原因、IP 与时间，不会记录密码。</div>
        </div>
        <div v-else>
          <div class="secret-box"><span style="flex:1">{{ revealed.password }}</span><CopyButton :text="revealed.password" tone="dark" /></div>
          <div class="hint" style="margin-top:8px">已记录审计。关闭后需重新提交原因才能再次查看。</div>
        </div>
      </div>
      <template #footer>
        <button class="btn" @click="closeReveal">{{ revealed ? '关闭' : '取消' }}</button>
        <button v-if="!revealed" class="btn btn-primary" :disabled="revealBusy" @click="submitReveal"><Loader2 v-if="revealBusy" :size="14" class="spin" /> 验证并查看</button>
      </template>
    </Modal>

    <!-- Reset -->
    <Modal :open="resetOpen" title="重置密码" :closable="!resetBusy" @close="resetOpen = false">
      <div v-if="!resetResult" class="stack">
        <div class="row"><div class="avatar" style="width:34px;height:34px">{{ resetTarget?.user_id.slice(0, 1).toUpperCase() }}</div><div><div style="font-weight:600">{{ resetTarget?.user_id }}</div><div class="small muted">旧密码和所有会话将立即失效</div></div></div>
        <div class="field"><label class="label">新密码</label><input v-model="resetPassword" class="input input-mono" placeholder="留空自动生成 24 位" /><div class="hint">8–128 字节；留空由服务端生成。</div></div>
        <div class="field"><label class="label">原因</label><input v-model="resetReason" class="input" placeholder="例如：用户忘记密码" maxlength="255" /></div>
        <div v-if="resetError" class="error-text">{{ resetError }}</div>
      </div>
      <div v-else class="stack">
        <div class="banner info"><Check :size="15" /> 密码已重置；用户已登录的会话已全部失效。</div>
        <div v-if="resetResult.password">
          <div class="label" style="margin-bottom:6px">新密码（仅本次显示）</div>
          <div class="secret-box"><span style="flex:1">{{ resetResult.password }}</span><CopyButton :text="resetResult.password" tone="dark" /></div>
        </div>
      </div>
      <template #footer>
        <button class="btn" @click="resetOpen = false">{{ resetResult ? '关闭' : '取消' }}</button>
        <button v-if="!resetResult" class="btn btn-danger" :disabled="resetBusy" @click="submitReset"><Loader2 v-if="resetBusy" :size="14" class="spin" /> 重置密码</button>
      </template>
    </Modal>

    <ConfirmDialog :open="confirmOpen" :title="confirmTitle" :text="confirmText" :danger="confirmKind === 'disable'" :busy="confirmBusy" :confirm-text="confirmKind === 'disable' ? '停用' : confirmKind === 'enable' ? '启用' : '吊销会话'" @close="confirmOpen = false" @confirm="doConfirm()" />
  </div>
</template>
