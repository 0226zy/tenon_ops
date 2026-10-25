<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { Activity, AlertTriangle, Loader2, Pause, Play, RefreshCw, UserRound } from 'lucide-vue-next'
import PageHeader from '@/components/PageHeader.vue'
import Badge from '@/components/Badge.vue'
import EmptyState from '@/components/EmptyState.vue'
import { api, ApiError } from '@/api/client'
import type { PresenceResult } from '@/api/types'
import { fmtInt, fmtNumber, fmtTime, tierLabel, tierTone, timeAgo } from '@/utils/format'

const data = ref<PresenceResult | null>(null)
const loading = ref(true)
const error = ref('')
const win = ref('15m')
const auto = ref(true)
let timer: number | undefined

const counts = computed(() => {
  const c = { running: 0, online: 0, active: 0, recent: 0, today: 0, idle: 0 }
  for (const item of data.value?.items || []) c[item.tier] = (c[item.tier] || 0) + 1
  return c
})

async function load(silent = false) {
  if (!silent) loading.value = true
  error.value = ''
  try { data.value = await api.presence(win.value) }
  catch (e) { error.value = e instanceof ApiError ? e.message : '加载活跃用户失败' }
  finally { loading.value = false }
}

function startTimer() {
  stopTimer()
  if (!auto.value) return
  timer = window.setInterval(() => { if (!document.hidden) void load(true) }, 10_000)
}
function stopTimer() { if (timer) { window.clearInterval(timer); timer = undefined } }
function toggleAuto() { auto.value = !auto.value; if (auto.value) startTimer(); else stopTimer() }
watch(win, () => load())
watch(auto, () => { if (auto.value) startTimer(); else stopTimer() })
onMounted(() => { void load(); startTimer() })
onBeforeUnmount(stopTimer)
</script>

<template>
  <div>
    <PageHeader title="活跃用户" subtitle="按业务活动计算，不是真实心跳。仅有会话、没有操作的用户显示为“未活跃”。">
      <template #actions>
        <span v-if="data" class="pill"><Activity :size="12" /> 数据截至 {{ fmtTime(data.as_of) }}</span>
        <div class="seg"><button v-for="w in ['5m','15m','60m','24h']" :key="w" :class="{ active: win === w }" @click="win = w">{{ w }}</button></div>
        <button class="btn" @click="toggleAuto"><Pause v-if="auto" :size="14" /><Play v-else :size="14" /> {{ auto ? '暂停' : '自动刷新' }}</button>
        <button class="btn" :disabled="loading" @click="load()"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button>
      </template>
    </PageHeader>

    <div v-if="error" class="banner danger" style="margin-bottom:14px"><AlertTriangle :size="15" /> {{ error }}<button class="link-btn" style="margin-left:auto" @click="load()">重试</button></div>

    <div class="grid-4" style="margin-bottom:16px">
      <div class="card stat"><div class="stat-label"><span class="dot" style="width:6px;height:6px;border-radius:50%;background:#8b5cf6" /> 运行中</div><div class="stat-value">{{ counts.running }}</div><div class="stat-meta">存在 Factive=1 的 Run</div></div>
      <div class="card stat"><div class="stat-label"><span class="dot" style="width:6px;height:6px;border-radius:50%;background:#22c55e" /> 在线</div><div class="stat-value">{{ counts.online }}</div><div class="stat-meta">最近 5 分钟内业务活动</div></div>
      <div class="card stat"><div class="stat-label"><span class="dot" style="width:6px;height:6px;border-radius:50%;background:#0ea5e9" /> 活跃</div><div class="stat-value">{{ counts.active }}</div><div class="stat-meta">5–15 分钟内有活动</div></div>
      <div class="card stat"><div class="stat-label"><UserRound :size="13" /> 当前窗口</div><div class="stat-value">{{ data?.items.length || 0 }}</div><div class="stat-meta">{{ win }} · 去重用户</div></div>
    </div>

    <section class="card">
      <div v-if="loading && !data" class="table-empty"><Loader2 :size="22" class="spin" /><div>加载活跃用户…</div></div>
      <div v-else-if="!data?.items.length" class="table-empty"><EmptyState title="该窗口没有活跃用户" text="用户在最近窗口内没有对话、Run 或模型请求；这并不等于他们从未登录。" /></div>
      <div v-else class="table-wrap">
        <table class="table">
          <thead><tr><th>用户</th><th>层级</th><th>最近活动</th><th class="right">活跃 Run</th><th class="right">会话</th><th class="right">窗口 Token</th><th class="right">24h Token</th><th>当前模型</th><th></th></tr></thead>
          <tbody><tr v-for="u in data.items" :key="u.user_id">
            <td><RouterLink :to="`/users/${encodeURIComponent(u.user_id)}`" class="mono" style="font-weight:600">{{ u.user_id }}</RouterLink><div class="small muted">{{ u.name }}</div></td>
            <td><Badge :tone="tierTone(u.tier)" dot>{{ tierLabel(u.tier) }}</Badge></td>
            <td><span class="small">{{ u.last_seen ? timeAgo(u.last_seen) : '—' }}</span><div class="small muted">{{ u.last_seen ? fmtTime(u.last_seen) : '' }}</div></td>
            <td class="right"><span :class="{ 'stat-accent': u.active_runs > 0 }">{{ u.active_runs }}</span></td>
            <td class="right">{{ fmtInt(u.sessions) }}</td>
            <td class="right mono">{{ fmtNumber(u.tokens_window) }}</td>
            <td class="right mono">{{ fmtNumber(u.tokens_24h) }}</td>
            <td class="mono small">{{ u.model || '—' }}</td>
            <td><RouterLink class="btn btn-sm" :to="`/users/${encodeURIComponent(u.user_id)}`">详情</RouterLink></td>
          </tr></tbody>
        </table>
      </div>
      <div v-if="data" class="table-foot"><span>按最近活动降序 · 最多 200 人</span><span>{{ auto ? '每 10 秒自动刷新' : '自动刷新已暂停' }}</span></div>
    </section>
  </div>
</template>
