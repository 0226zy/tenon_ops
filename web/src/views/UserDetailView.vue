<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  AlertTriangle, ArrowLeft, Check, ChevronRight, Eye, KeyRound, Loader2, Pencil,
  RefreshCw, ShieldCheck, ShieldOff, TimerReset, UserRound,
} from 'lucide-vue-next'
import PageHeader from '@/components/PageHeader.vue'
import Badge from '@/components/Badge.vue'
import StatCard from '@/components/StatCard.vue'
import Modal from '@/components/Modal.vue'
import StepUpModal from '@/components/StepUpModal.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import CopyButton from '@/components/CopyButton.vue'
import EmptyState from '@/components/EmptyState.vue'
import { api, ApiError } from '@/api/client'
import type { ResetResult, UserDetail } from '@/api/types'
import { fmtCost, fmtDuration, fmtInt, fmtNumber, fmtTime, pwdLabel, pwdTone, runLabel, runTone, timeAgo, userTone } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const toast = useToastStore()
const user = ref<UserDetail | null>(null)
const loading = ref(true)
const error = ref('')
const tab = ref('overview')

const revealOpen = ref(false)
const revealReason = ref('')
const revealBusy = ref(false)
const revealError = ref('')
const revealed = ref<{ password: string; expires_in: number } | null>(null)
const stepUpOpen = ref(false)
let revealTimer: number | undefined

const resetOpen = ref(false)
const resetReason = ref('')
const resetPassword = ref('')
const resetBusy = ref(false)
const resetError = ref('')
const resetResult = ref<ResetResult | null>(null)

const editOpen = ref(false)
const editBusy = ref(false)
const editError = ref('')
const edit = reactive({ name: '', max_conv: 20, tags: '', note: '' })

const confirmOpen = ref(false)
const confirmKind = ref<'disable' | 'enable' | 'revoke'>('disable')
const confirmBusy = ref(false)

const id = computed(() => String(route.params.id || ''))
const disabled = computed(() => user.value?.status === 2)

async function load(silent = false) {
  if (!silent) loading.value = true
  error.value = ''
  try {
    user.value = await api.users.get(id.value)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载用户失败'
  } finally {
    loading.value = false
  }
}

function startReveal() {
  revealReason.value = ''
  revealError.value = ''
  revealed.value = null
  if (!auth.elevated) stepUpOpen.value = true
  else revealOpen.value = true
}

function onStepUp() { stepUpOpen.value = false; revealOpen.value = true }

function closeReveal() {
  if (revealTimer) window.clearTimeout(revealTimer)
  revealTimer = undefined
  revealed.value = null
  revealOpen.value = false
}

async function submitReveal() {
  if (!user.value || revealBusy.value) return
  if (revealReason.value.trim().length < 4) { revealError.value = '请填写至少 4 个字符的原因。'; return }
  revealBusy.value = true
  revealError.value = ''
  try {
    const res = await api.users.reveal(user.value.user_id, revealReason.value.trim())
    revealed.value = res
    toast.success('密码已解密，30 秒后自动隐藏')
    if (revealTimer) window.clearTimeout(revealTimer)
    revealTimer = window.setTimeout(() => { revealed.value = null; revealOpen.value = false }, 30_000)
    await load(true)
  } catch (e) {
    if (e instanceof ApiError && e.code === 'STEP_UP_REQUIRED') { revealOpen.value = false; stepUpOpen.value = true }
    else revealError.value = e instanceof ApiError ? e.message : '查看失败'
  } finally { revealBusy.value = false }
}

function openReset() {
  resetReason.value = ''
  resetPassword.value = ''
  resetResult.value = null
  resetError.value = ''
  resetOpen.value = true
}

async function submitReset() {
  if (!user.value || resetBusy.value) return
  resetBusy.value = true
  resetError.value = ''
  try {
    resetResult.value = await api.users.reset(user.value.user_id, { password: resetPassword.value || undefined, reason: resetReason.value.trim() || '管理员重置' })
    toast.success('密码已重置')
    await load(true)
  } catch (e) { resetError.value = e instanceof ApiError ? e.message : '重置失败' }
  finally { resetBusy.value = false }
}

function openEdit() {
  if (!user.value) return
  edit.name = user.value.name
  edit.max_conv = user.value.max_conv
  edit.tags = user.value.tags.join(', ')
  edit.note = user.value.note
  editError.value = ''
  editOpen.value = true
}

async function submitEdit() {
  if (!user.value || editBusy.value) return
  editBusy.value = true
  editError.value = ''
  try {
    await api.users.patch(user.value.user_id, {
      name: edit.name.trim(), max_conv: Number(edit.max_conv) || 20,
      tags: edit.tags.split(',').map((v) => v.trim()).filter(Boolean), note: edit.note.trim(),
    })
    toast.success('用户资料已更新')
    editOpen.value = false
    await load(true)
  } catch (e) { editError.value = e instanceof ApiError ? e.message : '保存失败' }
  finally { editBusy.value = false }
}

function ask(kind: 'disable' | 'enable' | 'revoke') { confirmKind.value = kind; confirmOpen.value = true }

async function doConfirm() {
  if (!user.value || confirmBusy.value) return
  confirmBusy.value = true
  try {
    if (confirmKind.value === 'disable') await api.users.disable(user.value.user_id, '管理员停用')
    else if (confirmKind.value === 'enable') await api.users.enable(user.value.user_id)
    else await api.users.revokeSessions(user.value.user_id)
    toast.success('操作完成')
    confirmOpen.value = false
    await load(true)
  } catch (e) { toast.error(e instanceof ApiError ? e.message : '操作失败') }
  finally { confirmBusy.value = false }
}

const confirmTitle = computed(() => confirmKind.value === 'disable' ? '停用用户' : confirmKind.value === 'enable' ? '启用用户' : '吊销全部会话')
const confirmText = computed(() => {
  if (confirmKind.value === 'disable') return '停用后 Tenon 登录立即失败，现有会话失效；正在运行的 Run 不会被终止。'
  if (confirmKind.value === 'enable') return '启用后用户可用原密码登录。'
  return '该用户的所有浏览器会话将被提前过期，需要重新登录。'
})

watch(id, () => { tab.value = 'overview'; void load() })
onMounted(() => { void load() })
</script>

<template>
  <div>
    <div class="row" style="margin-bottom:12px">
      <RouterLink to="/users" class="btn btn-sm"><ArrowLeft :size="14" /> 用户列表</RouterLink>
      <span class="small muted">/</span>
      <span class="small muted">{{ id }}</span>
    </div>

    <div v-if="error" class="banner danger" style="margin-bottom:14px"><AlertTriangle :size="15" /> {{ error }}<button class="link-btn" style="margin-left:auto" @click="load()">重试</button></div>
    <div v-if="loading && !user" class="card card-pad"><div class="skeleton" style="height:26px;width:30%" /><div class="skeleton" style="height:14px;width:50%;margin-top:12px" /><div class="skeleton" style="height:80px;margin-top:20px" /></div>

    <template v-else-if="user">
      <div class="card card-pad" style="display:flex;gap:16px;align-items:flex-start;flex-wrap:wrap">
        <div class="avatar" style="width:48px;height:48px;font-size:18px;border-radius:12px">{{ user.user_id.slice(0, 1).toUpperCase() }}</div>
        <div style="min-width:0;flex:1">
          <div class="row wrap" style="gap:8px">
            <h1 style="font-size:20px;margin:0;letter-spacing:-.02em">{{ user.name }}</h1>
            <Badge :tone="userTone(user.status)" dot>{{ user.status === 1 ? '正常' : '已停用' }}</Badge>
            <Badge :tone="pwdTone(user.pwd_state)">{{ pwdLabel(user.pwd_state) }}</Badge>
          </div>
          <div class="row wrap" style="gap:8px;margin-top:5px">
            <span class="mono small muted">{{ user.user_id }}</span>
            <CopyButton :text="user.user_id" label="复制 ID" />
            <span v-for="t in user.tags" :key="t" class="tag">{{ t }}</span>
          </div>
          <div class="small muted" style="margin-top:6px">创建于 {{ fmtTime(user.created_at) }}<span v-if="user.created_by"> · by {{ user.created_by }}</span></div>
        </div>
        <div class="page-actions" style="margin-left:auto">
          <button class="btn" @click="load()"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button>
          <button class="btn" @click="openEdit"><Pencil :size="14" /> 编辑</button>
          <button class="btn" @click="openReset"><KeyRound :size="14" /> 重置密码</button>
          <button class="btn" @click="startReveal"><Eye :size="14" /> 查看密码</button>
          <button v-if="!disabled" class="btn btn-danger" @click="ask('disable')"><ShieldOff :size="14" /> 停用</button>
          <button v-else class="btn btn-primary" @click="ask('enable')"><ShieldCheck :size="14" /> 启用</button>
        </div>
      </div>

      <div class="grid-4" style="margin-top:16px">
        <StatCard label="活跃会话" :value="user.sessions"><template #icon><ShieldCheck :size="13" /></template><template #meta>已登录浏览器会话</template></StatCard>
        <StatCard label="活跃 Run" :value="user.active_runs" :tone="user.active_runs ? 'accent' : 'default'"><template #icon><TimerReset :size="13" /></template><template #meta>正在执行的任务</template></StatCard>
        <StatCard label="对话数" :value="user.conversations"><template #icon><UserRound :size="13" /></template><template #meta>未删除的对话</template></StatCard>
        <StatCard label="30 天 Token" :value="fmtNumber(user.tokens_30d)"><template #icon><Eye :size="13" /></template><template #meta>按 t_llm_usage 汇总</template></StatCard>
      </div>

      <div class="row" style="margin-top:20px;border-bottom:1px solid var(--line);gap:4px">
        <button v-for="t in [{k:'overview',l:'概览'},{k:'conversations',l:'对话'},{k:'runs',l:'任务'},{k:'usage',l:'用量'},{k:'security',l:'安全'}]" :key="t.k" class="btn btn-ghost" :style="tab === t.k ? 'color:var(--accent);box-shadow:inset 0 -2px 0 var(--accent);border-radius:0' : 'border-radius:0'" @click="tab = t.k">{{ t.l }}</button>
      </div>

      <div v-if="tab === 'overview'" class="grid-2" style="margin-top:16px">
        <section class="card">
          <div class="card-head"><h2 class="card-title">账号信息</h2></div>
          <div class="card-body">
            <dl class="kv">
              <dt>User ID</dt><dd class="mono">{{ user.user_id }}</dd>
              <dt>显示名</dt><dd>{{ user.name }}</dd>
              <dt>状态</dt><dd>{{ user.status_text }}</dd>
              <dt>最大会话数</dt><dd>{{ user.max_conv }}</dd>
              <dt>密码状态</dt><dd>{{ pwdLabel(user.pwd_state) }}</dd>
              <dt>最近活跃</dt><dd>{{ user.last_seen ? `${fmtTime(user.last_seen)}（${timeAgo(user.last_seen)}）` : '暂无业务活动' }}</dd>
              <dt>备注</dt><dd>{{ user.note || '—' }}</dd>
            </dl>
          </div>
        </section>
        <section class="card">
          <div class="card-head"><h2 class="card-title">模型用量（30 天）</h2><RouterLink to="/usage" class="small" style="margin-left:auto">用量分析</RouterLink></div>
          <div class="card-body">
            <div v-if="user.usage_by_model.length" class="stack">
              <div v-for="u in user.usage_by_model.slice(0, 6)" :key="u.model">
                <div class="row" style="justify-content:space-between;font-size:12px"><span class="mono">{{ u.model }}</span><span class="muted">{{ fmtNumber(u.total_tokens) }} tok · {{ fmtCost(u.cost) }}</span></div>
                <div class="meter" style="margin-top:5px"><span :style="{ width: `${Math.max(3, (u.total_tokens / Math.max(1, user.usage_by_model[0].total_tokens)) * 100)}%` }" /></div>
              </div>
            </div>
            <EmptyState v-else title="暂无模型用量" text="该用户还没有产生模型请求。" />
          </div>
        </section>
      </div>

      <section v-if="tab === 'overview'" class="card" style="margin-top:16px">
        <div class="card-head"><h2 class="card-title">最近任务</h2><RouterLink to="/runs" class="small" style="margin-left:auto">全部任务</RouterLink></div>
        <div v-if="user.recent_runs.length" class="table-wrap">
          <table class="table"><thead><tr><th>Run</th><th>模型</th><th>状态</th><th class="right">Token</th><th class="right">耗时</th><th>创建</th></tr></thead>
            <tbody><tr v-for="r in user.recent_runs" :key="r.run_id">
              <td><RouterLink :to="`/runs/${encodeURIComponent(r.run_id)}`" class="mono">{{ r.run_id.slice(0, 12) }}…</RouterLink></td>
              <td class="mono">{{ r.model }}</td>
              <td><Badge :tone="runTone(r.status_text)" dot>{{ runLabel(r) }}</Badge></td>
              <td class="right mono">{{ fmtNumber(r.total_tokens) }}</td>
              <td class="right small muted">{{ fmtDuration(r.duration_ms) }}</td>
              <td class="small muted">{{ fmtTime(r.created_at) }}</td>
            </tr></tbody></table>
        </div>
        <EmptyState v-else title="暂无任务" text="该用户还没有执行过 Agent 任务。" />
      </section>

      <section v-if="tab === 'conversations'" class="card" style="margin-top:16px">
        <div class="card-head"><h2 class="card-title">对话</h2></div>
        <div v-if="user.recent_conversations.length" class="table-wrap"><table class="table"><thead><tr><th>Conv ID</th><th>标题</th><th>模型</th><th>最近活跃</th><th>创建</th></tr></thead>
          <tbody><tr v-for="c in user.recent_conversations" :key="c.conv_id"><td class="mono">{{ c.conv_id }}</td><td>{{ c.title || '未命名' }}</td><td class="mono">{{ c.model }}</td><td class="small muted">{{ fmtTime(c.last_active_at) }}</td><td class="small muted">{{ fmtTime(c.created_at) }}</td></tr></tbody></table></div>
        <EmptyState v-else title="暂无对话" />
      </section>

      <section v-if="tab === 'runs'" class="card" style="margin-top:16px">
        <div class="card-head"><h2 class="card-title">最近任务</h2></div>
        <div v-if="user.recent_runs.length" class="table-wrap"><table class="table"><thead><tr><th>Run</th><th>状态</th><th>模型</th><th class="right">迭代</th><th class="right">Token</th><th class="right">耗时</th><th>创建</th></tr></thead>
          <tbody><tr v-for="r in user.recent_runs" :key="r.run_id"><td><RouterLink :to="`/runs/${encodeURIComponent(r.run_id)}`" class="mono">{{ r.run_id }}</RouterLink></td><td><Badge :tone="runTone(r.status_text)" dot>{{ runLabel(r) }}</Badge></td><td class="mono">{{ r.model }}</td><td class="right">{{ r.iterations }}</td><td class="right mono">{{ fmtNumber(r.total_tokens) }}</td><td class="right small muted">{{ fmtDuration(r.duration_ms) }}</td><td class="small muted">{{ fmtTime(r.created_at) }}</td></tr></tbody></table></div>
        <EmptyState v-else title="暂无任务" />
      </section>

      <section v-if="tab === 'usage'" class="card" style="margin-top:16px">
        <div class="card-head"><h2 class="card-title">按模型用量（30 天）</h2></div>
        <div v-if="user.usage_by_model.length" class="table-wrap"><table class="table"><thead><tr><th>模型</th><th class="right">请求</th><th class="right">Prompt</th><th class="right">Completion</th><th class="right">错误</th><th class="right">成本</th></tr></thead>
          <tbody><tr v-for="u in user.usage_by_model" :key="u.model"><td class="mono">{{ u.model }}</td><td class="right">{{ fmtInt(u.requests) }}</td><td class="right">{{ fmtNumber(u.prompt_tokens) }}</td><td class="right">{{ fmtNumber(u.completion_tokens) }}</td><td class="right">{{ fmtInt(u.errors) }}</td><td class="right">{{ fmtCost(u.cost) }}</td></tr></tbody></table></div>
        <EmptyState v-else title="暂无用量" />
      </section>

      <section v-if="tab === 'security'" class="grid-2" style="margin-top:16px">
        <div class="card"><div class="card-head"><h2 class="card-title">密码与审计</h2></div><div class="card-body"><dl class="kv">
          <dt>密码状态</dt><dd><Badge :tone="pwdTone(user.pwd_state)">{{ pwdLabel(user.pwd_state) }}</Badge></dd>
          <dt>最近查看</dt><dd>{{ user.last_reveal_at ? fmtTime(user.last_reveal_at) : '从未' }}</dd>
          <dt>查看次数</dt><dd>{{ user.reveal_count }}</dd>
          <dt>停用原因</dt><dd>{{ user.disabled_reason || '—' }}</dd>
          <dt>创建人</dt><dd>{{ user.created_by || '—' }}</dd>
        </dl><div class="row wrap" style="margin-top:16px"><button class="btn" @click="startReveal"><Eye :size="14" /> 查看密码</button><button class="btn" @click="openReset"><KeyRound :size="14" /> 重置密码</button><button class="btn" @click="ask('revoke')"><TimerReset :size="14" /> 吊销会话</button></div></div></div>
        <div class="card"><div class="card-head"><h2 class="card-title">安全说明</h2></div><div class="card-body stack small muted">
          <p style="margin:0">密码密文以 AES-256-GCM 存储在 <span class="mono">tadm_user_secret</span>，密钥在服务器 <span class="mono">master.key</span>，不在 MySQL。</p>
          <p style="margin:0">查看密码需要 step-up + 原因，并写入审计；不会记录密码本身。</p>
          <p style="margin:0">停用与重置会提前过期该用户全部 <span class="mono">t_auth_session</span>，但不会终止正在运行的 Run。</p>
          <div class="banner">服务器是信任边界：master.key 与 DB 同时失守可解密密码。</div>
        </div></div>
      </section>
    </template>

    <StepUpModal :open="stepUpOpen" subtitle="查看 Tenon 用户密码需要重新认证。" @close="stepUpOpen = false" @done="onStepUp" />

    <Modal :open="revealOpen" title="查看 Tenon 用户密码" :closable="!revealBusy" @close="closeReveal">
      <div class="stack">
        <div v-if="!revealed">
          <div class="field"><label class="label">查看原因（必填，写入审计）</label><input v-model="revealReason" class="input" placeholder="例如：交付给客户 A" maxlength="255" /></div>
          <div v-if="revealError" class="error-text" style="margin-top:8px">{{ revealError }}</div>
          <div class="banner" style="margin-top:12px">密码 30 秒后自动隐藏；审计只记录原因与操作者，不记录密码。</div>
        </div>
        <div v-else class="stack"><div class="secret-box"><span style="flex:1">{{ revealed.password }}</span><CopyButton :text="revealed.password" tone="dark" /></div><div class="hint">已记录审计。</div></div>
      </div>
      <template #footer><button class="btn" @click="closeReveal">{{ revealed ? '关闭' : '取消' }}</button><button v-if="!revealed" class="btn btn-primary" :disabled="revealBusy" @click="submitReveal"><Loader2 v-if="revealBusy" :size="14" class="spin" /> 验证并查看</button></template>
    </Modal>

    <Modal :open="resetOpen" title="重置密码" :closable="!resetBusy" @close="resetOpen = false">
      <div v-if="!resetResult" class="stack">
        <div class="field"><label class="label">新密码</label><input v-model="resetPassword" class="input input-mono" placeholder="留空自动生成 24 位" /><div class="hint">8–128 字节；旧密码与所有会话将立即失效。</div></div>
        <div class="field"><label class="label">原因</label><input v-model="resetReason" class="input" placeholder="例如：用户忘记密码" maxlength="255" /></div>
        <div v-if="resetError" class="error-text">{{ resetError }}</div>
      </div>
      <div v-else class="stack"><div class="banner info"><Check :size="15" /> 密码已重置，旧会话已失效。</div><div v-if="resetResult.password"><div class="label" style="margin-bottom:6px">新密码（仅本次显示）</div><div class="secret-box"><span style="flex:1">{{ resetResult.password }}</span><CopyButton :text="resetResult.password" tone="dark" /></div></div></div>
      <template #footer><button class="btn" @click="resetOpen = false">{{ resetResult ? '关闭' : '取消' }}</button><button v-if="!resetResult" class="btn btn-danger" :disabled="resetBusy" @click="submitReset"><Loader2 v-if="resetBusy" :size="14" class="spin" /> 重置</button></template>
    </Modal>

    <Modal :open="editOpen" title="编辑用户资料" :closable="!editBusy" @close="editOpen = false">
      <div class="stack">
        <div class="form-row"><div class="field"><label class="label">显示名</label><input v-model="edit.name" class="input" /></div><div class="field"><label class="label">最大会话数</label><input v-model.number="edit.max_conv" class="input" type="number" min="1" max="1000" /></div></div>
        <div class="field"><label class="label">标签</label><input v-model="edit.tags" class="input" placeholder="逗号分隔" /></div>
        <div class="field"><label class="label">备注</label><textarea v-model="edit.note" class="textarea" maxlength="255" /></div>
        <div v-if="editError" class="error-text">{{ editError }}</div>
      </div>
      <template #footer><button class="btn" :disabled="editBusy" @click="editOpen = false">取消</button><button class="btn btn-primary" :disabled="editBusy" @click="submitEdit"><Loader2 v-if="editBusy" :size="14" class="spin" /> 保存</button></template>
    </Modal>

    <ConfirmDialog :open="confirmOpen" :title="confirmTitle" :text="confirmText" :danger="confirmKind === 'disable'" :busy="confirmBusy" @close="confirmOpen = false" @confirm="doConfirm" />
  </div>
</template>
