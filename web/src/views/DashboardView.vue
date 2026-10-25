<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { Activity, AlertTriangle, ArrowUpRight, Coins, Cpu, Radio, RefreshCw, Users, Zap } from 'lucide-vue-next'
import PageHeader from '@/components/PageHeader.vue'
import StatCard from '@/components/StatCard.vue'
import Badge from '@/components/Badge.vue'
import EmptyState from '@/components/EmptyState.vue'
import { api, ApiError } from '@/api/client'
import type { Dashboard, UsagePoint } from '@/api/types'
import { fmtCost, fmtInt, fmtNumber, fmtSeconds, fmtTime, runLabel, runTone, shortID } from '@/utils/format'
import { useToastStore } from '@/stores/toast'

const data = ref<Dashboard | null>(null)
const loading = ref(true)
const error = ref('')
const toast = useToastStore()
let timer: number | undefined

const days = computed(() => {
  if (!data.value) return []
  const map = new Map<string, number>()
  for (const p of data.value.usage_series) {
    const key = p.day
    map.set(key, (map.get(key) || 0) + p.tokens)
  }
  const out = [...map.entries()].sort((a, b) => a[0].localeCompare(b[0])).slice(-24)
  const max = Math.max(1, ...out.map(([, v]) => v))
  return out.map(([day, value]) => ({ day, value, height: Math.max(4, Math.round((value / max) * 100)) }))
})

const runTotal = computed(() => {
  if (!data.value) return 0
  return Object.values(data.value.run_status_24h).reduce((a, b) => a + b, 0)
})

const runBars = computed(() => {
  if (!data.value) return []
  const total = runTotal.value || 1
  return [
    { key: 'finished', label: '完成', value: data.value.run_status_24h.finished || 0, color: '#16a34a' },
    { key: 'failed', label: '失败', value: data.value.run_status_24h.failed || 0, color: '#dc2626' },
    { key: 'running', label: '执行中', value: data.value.run_status_24h.running || 0, color: '#0284c7' },
    { key: 'cancelled', label: '取消', value: data.value.run_status_24h.cancelled || 0, color: '#94a3b8' },
    { key: 'other', label: '其他', value: Math.max(0, total - (data.value.run_status_24h.finished || 0) - (data.value.run_status_24h.failed || 0) - (data.value.run_status_24h.running || 0) - (data.value.run_status_24h.cancelled || 0)), color: '#f59e0b' },
  ].filter((v) => v.value > 0)
})

async function load(silent = false) {
  if (!silent) loading.value = true
  error.value = ''
  try {
    data.value = await api.dashboard()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : '加载失败'
    if (!silent) toast.error(error.value)
  } finally {
    loading.value = false
  }
}

function startTimer() {
  timer = window.setInterval(() => { if (!document.hidden) void load(true) }, 30_000)
}

onMounted(() => { void load(); startTimer() })
onBeforeUnmount(() => { if (timer) window.clearInterval(timer) })
</script>

<template>
  <div>
    <PageHeader title="总览" subtitle="Tenon 的身份、成本与运行状态。数据按业务活动计算，不伪造在线状态。">
      <template #actions>
        <span v-if="data" class="pill"><Radio :size="12" /> 数据截至 {{ fmtTime(data.data_through || data.as_of) }}</span>
        <button class="btn" :disabled="loading" @click="load()"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button>
      </template>
    </PageHeader>

    <div v-if="error" class="banner danger" style="margin-bottom:16px">
      <AlertTriangle :size="15" /> {{ error }}
      <button class="link-btn" style="margin-left:auto" @click="load()">重试</button>
    </div>

    <div v-if="loading && !data" class="grid-4">
      <div v-for="i in 4" :key="i" class="card stat"><div class="skeleton" style="height:12px;width:55%" /><div class="skeleton" style="height:30px;width:40%;margin-top:10px" /><div class="skeleton" style="height:10px;width:70%;margin-top:10px" /></div>
    </div>

    <template v-else-if="data">
      <div class="grid-4">
        <StatCard label="用户总数" :value="fmtInt(data.users_total)" tone="default">
          <template #icon><Users :size="13" /></template>
          <template #meta><span>{{ data.users_new_7d }} 新增 / 7 天</span><span>·</span><span>{{ data.users_unmanaged }} 密码未知</span></template>
        </StatCard>
        <StatCard label="活跃用户 · 15m" :value="fmtInt(data.active_users_15m)" tone="accent">
          <template #icon><Activity :size="13" /></template>
          <template #meta><span>{{ data.active_runs }} 个任务运行中</span></template>
        </StatCard>
        <StatCard label="今日 Token" :value="fmtNumber(data.today_tokens)" tone="default">
          <template #icon><Zap :size="13" /></template>
          <template #meta><span>{{ fmtInt(data.today_requests) }} 次模型请求</span><span>·</span><span>错误率 {{ ((data.error_rate || 0) * 100).toFixed(1) }}%</span></template>
        </StatCard>
        <StatCard label="今日估算成本" :value="data.today_unpriced_requests > 0 && data.today_priced_requests === 0 ? '未定价' : fmtCost(data.today_cost)" tone="warning">
          <template #icon><Coins :size="13" /></template>
          <template #meta><span>{{ data.today_unpriced_requests > 0 ? `${data.today_unpriced_requests} 个请求未定价` : '按模型价格表估算，非供应商账单' }}</span></template>
        </StatCard>
      </div>

      <div class="grid-2" style="margin-top:16px">
        <section class="card">
          <div class="card-head">
            <h2 class="card-title">24h Token 趋势</h2>
            <span class="small muted" style="margin-left:auto">按天（展示时区）</span>
          </div>
          <div class="card-body">
            <div v-if="days.length" class="chart">
              <div class="bars">
                <div v-for="d in days" :key="d.day" class="bar" :style="{ height: `${d.height}%` }" :title="`${d.day} · ${fmtInt(d.value)} tokens`" />
              </div>
              <div class="row" style="justify-content:space-between">
                <span class="small muted">{{ days[0]?.day }}</span>
                <span class="small muted">{{ days[days.length - 1]?.day }}</span>
              </div>
            </div>
            <EmptyState v-else title="暂无模型请求" text="当前时间范围内没有 t_llm_usage 记录。" />
          </div>
        </section>

        <section class="card">
          <div class="card-head"><h2 class="card-title">24h Run 状态</h2><span class="small muted" style="margin-left:auto">共 {{ runTotal }} 个</span></div>
          <div class="card-body">
            <div v-if="runBars.length" class="stack">
              <div v-for="b in runBars" :key="b.key">
                <div class="row" style="justify-content:space-between;font-size:12px;margin-bottom:5px">
                  <span>{{ b.label }}</span><span class="muted">{{ b.value }} · {{ ((b.value / (runTotal || 1)) * 100).toFixed(0) }}%</span>
                </div>
                <div class="meter"><span :style="{ width: `${(b.value / (runTotal || 1)) * 100}%`, background: b.color }" /></div>
              </div>
              <div class="divider" />
              <div class="row" style="justify-content:space-between"><span class="small muted">P95 耗时（24h）</span><strong>{{ fmtSeconds(data.p95_duration_seconds) }}</strong></div>
            </div>
            <EmptyState v-else title="暂无任务" text="当前时间范围内没有 Run 记录。" />
          </div>
        </section>
      </div>

      <div class="grid-3" style="margin-top:16px">
        <section class="card">
          <div class="card-head"><h2 class="card-title">模型用量 Top 5</h2><RouterLink to="/usage" class="small" style="margin-left:auto">用量分析 <ArrowUpRight :size="12" style="display:inline" /></RouterLink></div>
          <div class="card-body">
            <div v-if="data.top_models.length" class="rank">
              <div v-for="(m, i) in data.top_models" :key="m.model" class="rank-row">
                <span class="rank-num">{{ String(i + 1).padStart(2, '0') }}</span>
                <div style="min-width:0">
                  <div class="rank-name ellipsis mono">{{ m.model }}</div>
                  <div class="meter" style="margin-top:5px"><span :style="{ width: `${Math.max(4, (m.total_tokens / Math.max(1, data.top_models[0].total_tokens)) * 100)}%` }" /></div>
                </div>
                <span class="rank-value">{{ fmtNumber(m.total_tokens) }}</span>
              </div>
            </div>
            <EmptyState v-else title="暂无用量" text="导入模型目录并产生请求后显示。" />
          </div>
        </section>

        <section class="card">
          <div class="card-head"><h2 class="card-title">用户 Token Top 5</h2><RouterLink to="/users" class="small" style="margin-left:auto">用户管理 <ArrowUpRight :size="12" style="display:inline" /></RouterLink></div>
          <div class="card-body">
            <div v-if="data.top_users.length" class="rank">
              <div v-for="(u, i) in data.top_users" :key="u.user_id" class="rank-row">
                <span class="rank-num">{{ String(i + 1).padStart(2, '0') }}</span>
                <div style="min-width:0">
                  <RouterLink :to="`/users/${encodeURIComponent(u.user_id || '')}`" class="rank-name ellipsis mono">{{ u.user_id }}</RouterLink>
                  <div class="meter" style="margin-top:5px"><span :style="{ width: `${Math.max(4, (u.total_tokens / Math.max(1, data.top_users[0].total_tokens)) * 100)}%` }" /></div>
                </div>
                <span class="rank-value">{{ fmtNumber(u.total_tokens) }}</span>
              </div>
            </div>
            <EmptyState v-else title="暂无用户用量" text="开通用户并产生模型请求后显示。" />
          </div>
        </section>

        <section class="card">
          <div class="card-head"><h2 class="card-title">最近失败</h2><RouterLink to="/runs?status=failed" class="small" style="margin-left:auto">全部任务 <ArrowUpRight :size="12" style="display:inline" /></RouterLink></div>
          <div class="card-body">
            <div v-if="data.recent_failures.length" class="hr-list">
              <div v-for="r in data.recent_failures" :key="r.run_id" class="hr-item">
                <div style="min-width:0;flex:1">
                  <RouterLink :to="`/runs/${encodeURIComponent(r.run_id)}`" class="mono small" style="color:var(--ink);font-weight:600">{{ shortID(r.run_id, 14) }}</RouterLink>
                  <div class="small muted ellipsis">{{ r.user_name }} · {{ r.error_code || '未分类错误' }}</div>
                </div>
                <Badge :tone="runTone('failed')" dot>失败</Badge>
              </div>
            </div>
            <EmptyState v-else title="没有失败任务" text="最近没有失败的 Run。" />
          </div>
        </section>
      </div>

      <div class="row" style="margin-top:16px;color:var(--muted);font-size:11.5px">
        <Cpu :size="13" />
        <span>数据来源：Tenon MySQL · 用量成本为估算值；在线/活跃按业务活动分层，不代表真实心跳。</span>
      </div>
    </template>
  </div>
</template>
