<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { AlertTriangle, Coins, Download, Loader2, RefreshCw, Zap } from 'lucide-vue-next'
import PageHeader from '@/components/PageHeader.vue'
import StatCard from '@/components/StatCard.vue'
import EmptyState from '@/components/EmptyState.vue'
import Badge from '@/components/Badge.vue'
import StepUpModal from '@/components/StepUpModal.vue'
import { api, ApiError } from '@/api/client'
import type { UsageOverview } from '@/api/types'
import { fmtCost, fmtDuration, fmtInt, fmtNumber, fmtTime } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'

const auth = useAuthStore()
const toast = useToastStore()
const data = ref<UsageOverview | null>(null)
const loading = ref(true)
const error = ref('')
const groupBy = ref('model')
const from = ref('')
const to = ref('')
const userId = ref('')
const modelId = ref('')
const stepUpOpen = ref(false)
const pendingExport = ref(false)

const palette = ['#0f766e', '#0ea5e9', '#8b5cf6', '#f59e0b', '#ef4444', '#14b8a6', '#64748b']
const rangeLabel = computed(() => data.value ? `${fmtTime(data.value.from)} — ${fmtTime(data.value.to)}` : '')
const dimensionLabel = computed(() => groupBy.value === 'user' ? '用户' : groupBy.value === 'conv' ? '对话' : '模型')

function isoDaysAgo(days: number): string {
  const d = new Date()
  d.setDate(d.getDate() - days)
  return d.toISOString()
}
function isoNow(): string { return new Date().toISOString() }

function preset(days: number) {
  from.value = days === 0 ? new Date(new Date().setHours(0, 0, 0, 0)).toISOString() : isoDaysAgo(days)
  to.value = isoNow()
  void load()
}

function localInput(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  const off = d.getTimezoneOffset()
  return new Date(d.getTime() - off * 60000).toISOString().slice(0, 16)
}

function fromInput(v: string): string { return v ? new Date(v).toISOString() : '' }

const days = computed(() => {
  if (!data.value) return []
  const map = new Map<string, { total: number; parts: { model: string; value: number; color: string }[] }>()
  const models: string[] = []
  for (const p of data.value.series) {
    const model = p.model || 'all'
    if (!models.includes(model)) models.push(model)
    const entry = map.get(p.day) || { total: 0, parts: [] }
    entry.total += p.tokens
    entry.parts.push({ model, value: p.tokens, color: palette[models.indexOf(model) % palette.length] })
    map.set(p.day, entry)
  }
  const list = [...map.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  const max = Math.max(1, ...list.map(([, v]) => v.total))
  return list.map(([day, v]) => ({ day, total: v.total, height: Math.max(3, (v.total / max) * 100), parts: v.parts.map((p) => ({ ...p, height: v.total ? (p.value / v.total) * 100 : 0 })) }))
})

const legend = computed(() => {
  if (!data.value) return []
  const models = [...new Set(data.value.series.map((p) => p.model || 'all'))].slice(0, 7)
  return models.map((model, i) => ({ model, color: palette[i % palette.length] }))
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    data.value = await api.usage.overview({
      from: fromInput(from.value) || undefined, to: fromInput(to.value) || undefined,
      group_by: groupBy.value, user_id: userId.value.trim() || undefined, model: modelId.value.trim() || undefined,
    })
  } catch (e) { error.value = e instanceof ApiError ? e.message : '加载用量失败' }
  finally { loading.value = false }
}

function startExport() {
  if (!auth.elevated) { pendingExport.value = true; stepUpOpen.value = true; return }
  void doExport()
}

async function doExport() {
  try {
    await api.usage.export({ from: fromInput(from.value) || undefined, to: fromInput(to.value) || undefined, user_id: userId.value.trim() || undefined, model: modelId.value.trim() || undefined })
    toast.success('用量 CSV 已开始下载')
  } catch (e) { toast.error(e instanceof ApiError ? e.message : '导出失败') }
}

function onStepUp() {
  stepUpOpen.value = false
  if (pendingExport.value) { pendingExport.value = false; void doExport() }
}

function dimension(item: { model?: string; user_id?: string; conv_id?: string }): string {
  return item.user_id || item.conv_id || item.model || '—'
}

onMounted(() => { preset(7) })
</script>

<template>
  <div>
    <PageHeader title="用量分析" subtitle="按模型/用户/对话查看 token、错误率与估算成本。成本来自价格表，不是供应商账单。">
      <template #actions>
        <button class="btn" :disabled="loading" @click="load"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button>
        <button class="btn btn-primary" @click="startExport"><Download :size="14" /> 导出 CSV</button>
      </template>
    </PageHeader>

    <section class="card" style="padding:14px;margin-bottom:16px">
      <div class="toolbar">
        <div class="seg">
          <button @click="preset(0)">今日</button><button @click="preset(7)">7 天</button><button @click="preset(30)">30 天</button>
        </div>
        <input class="input" style="width:190px" type="datetime-local" :value="localInput(from)" @change="from = fromInput(($event.target as HTMLInputElement).value); load()" />
        <span class="small muted">至</span>
        <input class="input" style="width:190px" type="datetime-local" :value="localInput(to)" @change="to = fromInput(($event.target as HTMLInputElement).value); load()" />
        <select v-model="groupBy" class="select" style="width:auto" @change="load"><option value="model">按模型</option><option value="user">按用户</option><option value="conv">按对话</option></select>
        <input v-model="userId" class="input" style="width:150px" placeholder="user_id 过滤" @keyup.enter="load" />
        <input v-model="modelId" class="input" style="width:160px" placeholder="model 过滤" @keyup.enter="load" />
        <button class="btn" @click="load">查询</button>
      </div>
    </section>

    <div v-if="error" class="banner danger" style="margin-bottom:14px"><AlertTriangle :size="15" /> {{ error }}<button class="link-btn" style="margin-left:auto" @click="load">重试</button></div>
    <div v-if="loading && !data" class="card card-pad"><Loader2 :size="20" class="spin" /><span style="margin-left:8px">加载用量…</span></div>

    <template v-else-if="data">
      <div class="row wrap" style="margin-bottom:12px">
        <Badge :tone="data.peak_now ? 'amber' : 'green'" dot>{{ data.peak_now ? '当前高峰' : '当前非高峰' }}</Badge>
        <span class="small muted">计费时区 {{ data.timezone }} · 高峰为周一至周五 09:00–12:00、14:00–18:00；其他时间及周末非高峰</span>
      </div>
      <div class="grid-4">
        <StatCard label="模型请求" :value="fmtInt(data.total.requests)"><template #icon><Zap :size="13" /></template><template #meta>{{ rangeLabel }}</template></StatCard>
        <StatCard label="总 Token" :value="fmtNumber(data.total.total_tokens)" tone="accent"><template #icon><Zap :size="13" /></template><template #meta>prompt {{ fmtNumber(data.total.prompt_tokens) }} / completion {{ fmtNumber(data.total.completion_tokens) }}</template></StatCard>
        <StatCard label="估算成本" :value="fmtCost(data.total.cost)" tone="warning"><template #icon><Coins :size="13" /></template><template #meta>{{ data.total.unpriced_requests ? `${data.total.unpriced_requests} 个请求未定价` : '全部请求已定价' }}</template></StatCard>
        <StatCard label="错误率 / 平均耗时" :value="`${((data.total.error_rate || 0) * 100).toFixed(1)}%`"><template #meta>{{ fmtDuration(data.total.avg_ms) }} 平均代理耗时 · {{ fmtInt(data.total.errors) }} 个错误</template></StatCard>
      </div>

      <div class="grid-2" style="margin-top:16px">
        <section class="card"><div class="card-head"><h2 class="card-title">Token 趋势（按天）</h2><span class="small muted" style="margin-left:auto">{{ data.group_by }}</span></div>
          <div class="card-body">
            <div v-if="days.length" class="chart">
              <div class="bars" style="height:190px">
                <div v-for="d in days" :key="d.day" class="bar-stack" style="flex:1;min-width:4px" :title="`${d.day} · ${fmtInt(d.total)} tokens`">
                  <div v-for="p in d.parts" :key="p.model" :style="{ height: `${Math.max(1, (p.height / 100) * d.height)}%`, background: p.color, borderRadius: '2px' }" />
                </div>
              </div>
              <div class="row" style="justify-content:space-between"><span class="small muted">{{ days[0]?.day }}</span><span class="small muted">{{ days[days.length - 1]?.day }}</span></div>
              <div class="bar-legend"><span v-for="l in legend" :key="l.model"><span class="legend-dot" :style="{ background: l.color }" />{{ l.model }}</span></div>
            </div>
            <EmptyState v-else title="没有用量" text="调整时间范围或过滤条件。" />
          </div>
        </section>
        <section class="card"><div class="card-head"><h2 class="card-title">模型占比</h2></div>
          <div class="card-body"><div v-if="data.items.length" class="rank">
            <div v-for="(m, i) in data.items.slice(0, 8)" :key="dimension(m)" class="rank-row">
              <span class="rank-num">{{ String(i + 1).padStart(2, '0') }}</span>
              <div style="min-width:0"><div class="rank-name mono ellipsis">{{ dimension(m) }}</div><div class="meter" style="margin-top:5px"><span :style="{ width: `${Math.max(3, (m.total_tokens / Math.max(1, data.items[0].total_tokens)) * 100)}%`, background: palette[i % palette.length] }" /></div></div>
              <span class="rank-value">{{ fmtNumber(m.total_tokens) }}</span>
            </div>
          </div><EmptyState v-else title="没有数据" /></div>
        </section>
      </div>

      <section class="card" style="margin-top:16px">
        <div class="card-head"><h2 class="card-title">明细（{{ dimensionLabel }}）</h2><span class="small muted" style="margin-left:auto">按总 token 降序</span></div>
        <div v-if="data.items.length" class="table-wrap"><table class="table">
          <thead><tr><th>{{ dimensionLabel }}</th><th class="right">请求</th><th class="right">Prompt</th><th class="right">Completion</th><th class="right">错误率</th><th class="right">平均耗时</th><th class="right">估算成本</th><th class="right">未定价</th></tr></thead>
          <tbody><tr v-for="m in data.items" :key="dimension(m)">
            <td class="mono">{{ dimension(m) }}</td><td class="right">{{ fmtInt(m.requests) }}</td><td class="right">{{ fmtNumber(m.prompt_tokens) }}</td><td class="right">{{ fmtNumber(m.completion_tokens) }}</td><td class="right">{{ ((m.error_rate || 0) * 100).toFixed(1) }}%</td><td class="right small muted">{{ fmtDuration(m.avg_ms) }}</td><td class="right">{{ fmtCost(m.cost) }}</td><td class="right">{{ m.unpriced_requests || '—' }}</td>
          </tr></tbody>
        </table></div>
        <EmptyState v-else title="该区间没有模型请求" text="切换时间范围，或确认 Tenon 已经产生 t_llm_usage 记录。" />
      </section>
    </template>

    <StepUpModal :open="stepUpOpen" subtitle="导出用量明细需要重新认证。" @close="stepUpOpen = false" @done="onStepUp" />
  </div>
</template>
