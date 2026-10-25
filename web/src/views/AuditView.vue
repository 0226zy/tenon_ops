<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { AlertTriangle, Download, Loader2, RefreshCw, ScrollText, Search } from 'lucide-vue-next'
import PageHeader from '@/components/PageHeader.vue'
import Badge from '@/components/Badge.vue'
import Modal from '@/components/Modal.vue'
import EmptyState from '@/components/EmptyState.vue'
import StepUpModal from '@/components/StepUpModal.vue'
import { api, ApiError } from '@/api/client'
import type { AuditEntry } from '@/api/types'
import { fmtTime, timeAgo } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'

const auth = useAuthStore()
const toast = useToastStore()
const items = ref<AuditEntry[]>([])
const actions = ref<string[]>([])
const next = ref('')
const loading = ref(true)
const error = ref('')
const admin = ref('')
const action = ref('')
const target = ref('')
const result = ref('')
const detail = ref<AuditEntry | null>(null)
const detailOpen = ref(false)
const stepUpOpen = ref(false)
const pendingExport = ref(false)

const hasMore = computed(() => !!next.value)

async function load(more = false) {
  if (!more) loading.value = true
  error.value = ''
  try {
    const page = await api.audit.list({
      admin_id: admin.value.trim() || undefined, action: action.value || undefined,
      target: target.value.trim() || undefined, result: result.value === '' ? undefined : Number(result.value),
      cursor: more ? next.value : undefined, limit: 50,
    })
    items.value = more ? [...items.value, ...page.items] : page.items
    next.value = page.next_cursor
  } catch (e) { error.value = e instanceof ApiError ? e.message : '加载审计失败' }
  finally { loading.value = false }
}

async function loadActions() {
  try { actions.value = (await api.audit.actions()).items } catch { /* optional */ }
}

function openDetail(e: AuditEntry) { detail.value = e; detailOpen.value = true }

function startExport() {
  if (!auth.elevated) { pendingExport.value = true; stepUpOpen.value = true; return }
  void doExport()
}
async function doExport() {
  try {
    await api.audit.export({ admin_id: admin.value.trim() || undefined, action: action.value || undefined, target: target.value.trim() || undefined })
    toast.success('审计 CSV 已开始下载')
  } catch (e) { toast.error(e instanceof ApiError ? e.message : '导出失败') }
}
function onStepUp() { stepUpOpen.value = false; if (pendingExport.value) { pendingExport.value = false; void doExport() } }

onMounted(() => { void load(); void loadActions() })
</script>

<template>
  <div>
    <PageHeader title="审计日志" subtitle="写操作、登录、查看密码与导出都会留痕；审计只追加，不记录密码/密文/密钥。">
      <template #actions>
        <button class="btn" :disabled="loading" @click="load()"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button>
        <button class="btn btn-primary" @click="startExport"><Download :size="14" /> 导出 CSV</button>
      </template>
    </PageHeader>

    <section class="card">
      <div class="table-toolbar">
        <div class="search"><Search :size="15" /><input v-model="admin" class="input" placeholder="管理员 ID" @keyup.enter="load()" /></div>
        <select v-model="action" class="select" style="width:auto" @change="load()"><option value="">全部动作</option><option v-for="a in actions" :key="a" :value="a">{{ a }}</option></select>
        <input v-model="target" class="input" style="width:170px" placeholder="目标 user/model/..." @keyup.enter="load()" />
        <select v-model="result" class="select" style="width:auto" @change="load()"><option value="">全部结果</option><option value="1">成功</option><option value="0">失败</option></select>
        <button class="btn" @click="load()">筛选</button>
        <span class="spacer" />
        <span class="small muted">{{ items.length }} 条{{ hasMore ? '（还有更多）' : '' }}</span>
      </div>
      <div v-if="error" class="banner danger" style="margin:14px"><AlertTriangle :size="15" /> {{ error }}<button class="link-btn" style="margin-left:auto" @click="load()">重试</button></div>
      <div v-if="loading && !items.length" class="table-empty"><Loader2 :size="22" class="spin" /><div>加载审计…</div></div>
      <div v-else-if="!items.length" class="table-empty"><EmptyState title="没有审计记录" text="执行一次写操作后，这里会出现记录。" /></div>
      <div v-else class="table-wrap">
        <table class="table">
          <thead><tr><th>时间</th><th>管理员</th><th>动作</th><th>目标</th><th>结果</th><th>来源 IP</th><th>request_id</th><th></th></tr></thead>
          <tbody><tr v-for="e in items" :key="e.id" class="clickable" @click="openDetail(e)">
            <td class="small"><div>{{ fmtTime(e.ts) }}</div><div class="small muted">{{ timeAgo(e.ts) }}</div></td>
            <td class="mono small">{{ e.admin_id || '—' }}</td>
            <td><Badge :tone="e.action.includes('failure') || e.action.includes('reveal') ? 'amber' : 'gray'">{{ e.action }}</Badge></td>
            <td class="small"><span class="mono">{{ e.target_type }}{{ e.target_id ? `:${e.target_id}` : '' }}</span><div v-if="e.reason" class="small muted ellipsis" style="max-width:240px">{{ e.reason }}</div></td>
            <td><Badge :tone="e.result === 1 ? 'green' : 'red'" dot>{{ e.result === 1 ? '成功' : '失败' }}</Badge></td>
            <td class="mono small">{{ e.ip }}</td>
            <td class="mono small muted">{{ e.request_id.slice(0, 12) }}</td>
            <td><ScrollText :size="14" style="color:var(--muted)" /></td>
          </tr></tbody>
        </table>
      </div>
      <div v-if="hasMore" class="table-foot"><span>继续加载更早的审计</span><button class="btn btn-sm" @click="load(true)">加载更多</button></div>
    </section>

    <Modal :open="detailOpen" title="审计详情" wide @close="detailOpen = false">
      <div v-if="detail" class="stack">
        <dl class="kv">
          <dt>时间</dt><dd>{{ fmtTime(detail.ts) }}</dd>
          <dt>管理员</dt><dd class="mono">{{ detail.admin_id || '—' }}</dd>
          <dt>动作</dt><dd class="mono">{{ detail.action }}</dd>
          <dt>目标</dt><dd class="mono">{{ detail.target_type }}{{ detail.target_id ? `:${detail.target_id}` : '' }}</dd>
          <dt>结果</dt><dd><Badge :tone="detail.result === 1 ? 'green' : 'red'">{{ detail.result === 1 ? '成功' : '失败' }}</Badge></dd>
          <dt>原因</dt><dd>{{ detail.reason || '—' }}</dd>
          <dt>IP / UA</dt><dd class="mono small">{{ detail.ip }}<br />{{ detail.ua }}</dd>
          <dt>request_id</dt><dd class="mono small">{{ detail.request_id }}</dd>
        </dl>
        <div><div class="label" style="margin-bottom:6px">detail（脱敏）</div><pre class="code">{{ detail.detail ? JSON.stringify(detail.detail, null, 2) : '—' }}</pre></div>
      </div>
    </Modal>

    <StepUpModal :open="stepUpOpen" subtitle="导出审计日志需要重新认证。" @close="stepUpOpen = false" @done="onStepUp" />
  </div>
</template>
