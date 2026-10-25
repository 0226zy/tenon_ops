<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  AlertTriangle, ArrowLeft, ChevronDown, ChevronRight, CircleDot, Copy, Loader2, Play, RefreshCw,
  Square, Terminal, Wrench,
} from 'lucide-vue-next'
import Badge from '@/components/Badge.vue'
import CopyButton from '@/components/CopyButton.vue'
import EmptyState from '@/components/EmptyState.vue'
import { api, ApiError } from '@/api/client'
import type { Event, RunDetail } from '@/api/types'
import { fmtCost, fmtDuration, fmtNumber, fmtTime, jsonPretty, runLabel, runTone } from '@/utils/format'
import { useToastStore } from '@/stores/toast'

interface Payload {
  kind?: string
  message?: {
    role?: string
    content?: string
    tool_calls?: { id: string; function: { name: string; arguments: string } }[]
    tool_call_id?: string
    name?: string
    is_error?: boolean
  }
  marker?: { type?: string; detail?: string; error_code?: string; iterations?: number; prompt_tokens?: number; completion_tokens?: number }
  note?: string
  [key: string]: unknown
}

const route = useRoute()
const toast = useToastStore()
const detail = ref<RunDetail | null>(null)
const events = ref<Event[]>([])
const next = ref('')
const loading = ref(true)
const eventsLoading = ref(false)
const error = ref('')
const follow = ref(true)
const expanded = ref<Set<string>>(new Set())
const rawOpen = ref<Set<string>>(new Set())
const eventHasMore = ref(false)
let timer: number | undefined

const id = computed(() => String(route.params.id || ''))
const run = computed(() => detail.value?.run || null)
const usageTotal = computed(() => (detail.value?.usage || []).reduce((acc, u) => ({ prompt: acc.prompt + u.prompt_tokens, completion: acc.completion + u.completion_tokens, cost: acc.cost + u.cost }), { prompt: 0, completion: 0, cost: 0 }))
const messages = computed(() => detail.value?.messages || [])
const userMessage = computed(() => messages.value.find((m) => m.role === 1))
const assistantMessage = computed(() => [...messages.value].reverse().find((m) => m.role === 2))

function parsePayload(e: Event): Payload {
  if (typeof e.payload === 'string') {
    try { return JSON.parse(e.payload) as Payload } catch { return { note: e.payload } }
  }
  if (e.payload && typeof e.payload === 'object') return e.payload as Payload
  return {}
}

function keyOf(e: Event, i = 0): string { return `${e.seq}:${i}` }
function isExpanded(e: Event, i = 0): boolean { return expanded.value.has(keyOf(e, i)) }
function toggle(e: Event, i = 0): void {
  const s = new Set(expanded.value)
  const k = keyOf(e, i)
  if (s.has(k)) s.delete(k); else s.add(k)
  expanded.value = s
}
function isRaw(e: Event): boolean { return rawOpen.value.has(String(e.seq)) }
function toggleRaw(e: Event): void {
  const s = new Set(rawOpen.value)
  if (s.has(String(e.seq))) s.delete(String(e.seq)); else s.add(String(e.seq))
  rawOpen.value = s
}

function controlTitle(type: string): string {
  const map: Record<string, string> = {
    run_accepted: '任务已接受', run_started: '开始执行', run_cancelling: '请求取消',
    run_finalizing: '开始收尾', run_finished: '任务完成', run_failed: '任务失败', run_cancelled: '任务已取消',
  }
  return map[type] || type
}
function controlTone(type: string): string {
  if (type === 'run_failed') return 'error'
  if (type === 'run_finished') return 'result'
  return 'control'
}
function markerTitle(type?: string): string {
  if (type === 'turn_done') return '回合完成'
  if (type === 'turn_failed') return '回合失败'
  if (type === 'turn_cancelled') return '回合取消'
  if (type === 'progress') return '进度'
  return type || 'marker'
}
function toolName(call: { function: { name: string } }): string { return call.function?.name || 'tool' }
function toolArgs(call: { function: { arguments: string } }): string { return call.function?.arguments || '' }
function textPreview(v: string, n = 1800): string { return v.length > n ? `${v.slice(0, n)}\n…（已截断）` : v }
function payloadSummary(e: Event): string {
  const p = parsePayload(e)
  if (p.marker?.detail) return String(p.marker.detail)
  if (p.marker?.error_code) return `error: ${p.marker.error_code}`
  const raw = typeof e.payload === 'string' ? e.payload : JSON.stringify(e.payload)
  return raw.length > 300 ? `${raw.slice(0, 300)}…` : raw
}

async function load(silent = false) {
  if (!silent) loading.value = true
  error.value = ''
  try {
    detail.value = await api.runs.get(id.value)
    await loadEvents(true)
  } catch (e) { error.value = e instanceof ApiError ? e.message : '加载任务失败' }
  finally { loading.value = false; schedule() }
}

async function loadEvents(reset = false, more = false) {
  eventsLoading.value = true
  try {
    const page = await api.runs.events(id.value, reset ? 0 : Number(next.value || 0), 100)
    events.value = reset || !more ? page.items : [...events.value, ...page.items]
    next.value = page.next_cursor
    eventHasMore.value = page.items.length >= 100
    if (reset) {
      // auto-expand the terminal/error event and open raw for the first event
      const first = events.value[0]
      if (first) rawOpen.value = new Set([String(first.seq)])
    }
  } catch (e) { if (!more) throw e; toast.error(e instanceof ApiError ? e.message : '加载轨迹失败') }
  finally { eventsLoading.value = false }
}

function schedule() {
  if (timer) window.clearInterval(timer)
  if (!run.value?.active || !follow.value) return
  timer = window.setInterval(async () => {
    if (document.hidden) return
    try {
      await loadEvents(false, true)
      const fresh = await api.runs.get(id.value)
      detail.value = fresh
      if (!fresh.run.active) {
        follow.value = false
        if (timer) window.clearInterval(timer)
        toast.info('任务已结束，实时跟随已停止')
      }
    } catch { /* keep last data */ }
  }, 2000)
}

watch(id, () => { events.value = []; next.value = ''; void load() })
onMounted(load)
onBeforeUnmount(() => { if (timer) window.clearInterval(timer) })
</script>

<template>
  <div>
    <div class="row" style="margin-bottom:12px">
      <RouterLink to="/runs" class="btn btn-sm"><ArrowLeft :size="14" /> 任务列表</RouterLink>
      <span class="small muted">/</span><span class="small mono muted">{{ id }}</span>
    </div>

    <div v-if="error" class="banner danger" style="margin-bottom:14px"><AlertTriangle :size="15" /> {{ error }}<button class="link-btn" style="margin-left:auto" @click="load()">重试</button></div>
    <div v-if="loading && !run" class="card card-pad"><Loader2 :size="20" class="spin" /><span style="margin-left:8px">加载任务…</span></div>

    <template v-else-if="run && detail">
      <div class="card card-pad">
        <div class="row wrap" style="align-items:flex-start;gap:12px">
          <div style="min-width:0;flex:1">
            <div class="row wrap" style="gap:8px"><h1 class="mono" style="font-size:18px;margin:0;letter-spacing:-.01em">{{ run.run_id }}</h1><Badge :tone="runTone(run.status_text)" dot>{{ runLabel(run) }}</Badge><Badge v-if="run.active" tone="violet" dot>实时</Badge><CopyButton :text="run.run_id" /></div>
            <div class="small muted" style="margin-top:6px">
              <RouterLink :to="`/users/${encodeURIComponent(run.user_id)}`">{{ run.user_name }}</RouterLink> ·
              {{ run.conv_title || run.conv_id }} · model <span class="mono">{{ run.model }}</span>
            </div>
          </div>
          <div class="page-actions" style="margin-left:auto">
            <button class="btn" :disabled="loading" @click="load()"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button>
            <button class="btn" :class="{ 'btn-primary': follow && run.active }" :disabled="!run.active" @click="follow = !follow; schedule()"><Square v-if="follow && run.active" :size="13" /><Play v-else :size="13" /> {{ follow && run.active ? '停止跟随' : '跟随实时' }}</button>
          </div>
        </div>
        <div class="grid-4" style="margin-top:18px">
          <div><div class="stat-label">耗时</div><div style="font-weight:650;margin-top:4px">{{ fmtDuration(run.duration_ms) }}</div></div>
          <div><div class="stat-label">迭代 / Token</div><div style="font-weight:650;margin-top:4px">{{ run.iterations }} · {{ fmtNumber(run.total_tokens) }}</div></div>
          <div><div class="stat-label">Run 汇总 token</div><div style="font-weight:650;margin-top:4px">{{ fmtNumber(run.prompt_tokens) }} + {{ fmtNumber(run.completion_tokens) }}</div></div>
          <div><div class="stat-label">用量明细成本</div><div style="font-weight:650;margin-top:4px">{{ fmtCost(usageTotal.cost) }}</div></div>
        </div>
        <div v-if="run.error_code || run.error_message" class="banner danger" style="margin-top:14px"><AlertTriangle :size="15" /><span><strong>{{ run.error_code }}</strong><span v-if="run.error_message"> — {{ run.error_message }}</span></span></div>
      </div>

      <div class="grid-2" style="margin-top:16px;align-items:start">
        <section class="card">
          <div class="card-head"><h2 class="card-title">输入与最终回答</h2></div>
          <div class="card-body stack">
            <div v-if="userMessage"><div class="small muted">用户输入</div><div class="tl-content" style="background:#fff">{{ userMessage.content }}</div></div>
            <div v-if="assistantMessage"><div class="small muted">最终回答</div><div class="tl-content" style="background:#fff">{{ assistantMessage.content }}</div></div>
            <EmptyState v-if="!userMessage && !assistantMessage" title="没有消息记录" />
          </div>
        </section>
        <section class="card">
          <div class="card-head"><h2 class="card-title">用量明细</h2><span class="small muted" style="margin-left:auto">{{ detail.usage.length }} 次请求</span></div>
          <div v-if="detail.usage.length" class="table-wrap"><table class="table"><thead><tr><th>时间</th><th>模型</th><th class="right">Prompt</th><th class="right">Completion</th><th class="right">耗时</th><th class="right">成本</th><th>状态</th></tr></thead>
            <tbody><tr v-for="(u, i) in detail.usage" :key="i"><td class="small muted">{{ fmtTime(u.created_at) }}</td><td class="mono small">{{ u.model }}</td><td class="right">{{ fmtNumber(u.prompt_tokens) }}</td><td class="right">{{ fmtNumber(u.completion_tokens) }}</td><td class="right small muted">{{ fmtDuration(u.cost_ms) }}</td><td class="right">{{ u.priced ? fmtCost(u.cost) : '未定价' }}</td><td><Badge :tone="u.http_status === 200 ? 'green' : u.http_status === 0 ? 'red' : 'amber'">{{ u.http_status }}</Badge></td></tr></tbody></table></div>
          <EmptyState v-else title="没有模型请求" />
        </section>
      </div>

      <section class="card" style="margin-top:16px">
        <div class="card-head"><h2 class="card-title">执行轨迹</h2><span class="small muted" style="margin-left:auto">{{ events.length }} 条事件</span></div>
        <div class="card-body">
          <div v-if="!events.length" class="table-empty"><EmptyState title="暂无事件" text="该 Run 还没有写入 t_event。" /></div>
          <div v-else class="timeline">
            <div v-for="e in events" :key="e.seq" class="tl-item">
              <div class="tl-time">{{ new Date(e.created_at).toLocaleTimeString('zh-CN', { hour12: false }) }}</div>
              <div class="tl-rail"><span class="tl-dot" :class="e.source === 1 ? controlTone(e.type) : (parsePayload(e).message?.role === 'tool' ? (parsePayload(e).message?.is_error ? 'error' : 'result') : parsePayload(e).message ? 'text' : parsePayload(e).marker ? 'marker' : 'control')" /></div>
              <div class="tl-body">
                <div class="tl-head">
                  <span class="tl-title">
                    <template v-if="e.source === 1">{{ controlTitle(e.type) }}</template>
                    <template v-else-if="parsePayload(e).message?.role === 'assistant'">assistant</template>
                    <template v-else-if="parsePayload(e).message?.role === 'tool'">{{ parsePayload(e).message?.name || 'tool' }} 结果</template>
                    <template v-else-if="parsePayload(e).marker">{{ markerTitle(parsePayload(e).marker?.type) }}</template>
                    <template v-else-if="parsePayload(e).note">notice</template>
                    <template v-else>{{ e.type }}</template>
                  </span>
                  <span class="tl-meta">#{{ e.seq }}</span>
                  <span v-if="e.truncated" class="badge badge-amber">payload 已截断</span>
                  <button class="btn btn-ghost btn-sm" style="margin-left:auto" @click="toggleRaw(e)">{{ isRaw(e) ? '收起原始' : '原始 JSON' }}</button>
                </div>

                <template v-if="e.source === 1">
                  <div class="tl-content">{{ payloadSummary(e) }}</div>
                </template>
                <template v-else>
                  <template v-if="parsePayload(e).message?.role === 'assistant'">
                    <div v-if="parsePayload(e).message?.content" class="tl-text" style="margin-top:6px">{{ textPreview(parsePayload(e).message?.content || '') }}</div>
                    <div v-for="(call, ci) in (parsePayload(e).message?.tool_calls || [])" :key="call.id" class="tl-content tl-tool" style="margin-top:6px">
                      <div class="row" style="gap:6px"><Wrench :size="13" /><strong class="mono">{{ toolName(call) }}</strong><span class="tl-meta">{{ call.id }}</span></div>
                      <pre style="margin-top:6px">{{ isExpanded(e, ci) ? toolArgs(call) : textPreview(toolArgs(call), 500) }}</pre>
                      <button v-if="toolArgs(call).length > 500" class="link-btn small" style="margin-top:6px" @click="toggle(e, ci)">{{ isExpanded(e, ci) ? '收起参数' : '展开参数' }}</button>
                    </div>
                  </template>
                  <template v-else-if="parsePayload(e).message?.role === 'tool'">
                    <div class="tl-content" :class="parsePayload(e).message?.is_error ? 'tl-error' : 'tl-result'">
                      <div class="row" style="gap:6px"><CircleDot :size="12" /><span class="mono small">{{ parsePayload(e).message?.name || 'tool' }}</span><span v-if="parsePayload(e).message?.tool_call_id" class="tl-meta">{{ parsePayload(e).message?.tool_call_id }}</span><span v-if="parsePayload(e).message?.is_error" class="badge badge-red">错误</span></div>
                      <pre style="margin-top:6px">{{ isExpanded(e) ? (parsePayload(e).message?.content || '') : textPreview(parsePayload(e).message?.content || '') }}</pre>
                      <button v-if="(parsePayload(e).message?.content || '').length > 1800" class="link-btn small" style="margin-top:6px" @click="toggle(e)">{{ isExpanded(e) ? '收起结果' : '展开结果' }}</button>
                    </div>
                  </template>
                  <template v-else-if="parsePayload(e).marker">
                    <div class="tl-content" :class="parsePayload(e).marker?.type === 'turn_failed' ? 'tl-error' : ''">
                      <div class="row wrap small"><span class="mono">{{ parsePayload(e).marker?.type }}</span><span v-if="parsePayload(e).marker?.detail" class="muted">{{ parsePayload(e).marker?.detail }}</span><span v-if="parsePayload(e).marker?.error_code" class="badge badge-red">{{ parsePayload(e).marker?.error_code }}</span><span v-if="parsePayload(e).marker?.iterations" class="muted">iterations {{ parsePayload(e).marker?.iterations }}</span></div>
                    </div>
                  </template>
                  <template v-else-if="parsePayload(e).note">
                    <div class="tl-content">{{ parsePayload(e).note }}</div>
                  </template>
                  <template v-else>
                    <div class="tl-content">{{ payloadSummary(e) }}</div>
                  </template>
                </template>

                <div v-if="isRaw(e)" class="code" style="margin-top:8px">{{ jsonPretty(e.payload) }}</div>
              </div>
            </div>
          </div>
          <div v-if="eventHasMore" class="table-foot" style="border-top:1px solid var(--line);margin-top:8px"><span>还有更早的事件</span><button class="btn btn-sm" :disabled="eventsLoading" @click="loadEvents(false, true)"><Loader2 v-if="eventsLoading" :size="13" class="spin" /> 加载更多</button></div>
        </div>
      </section>

      <div class="row" style="margin-top:14px;color:var(--muted);font-size:11.5px"><Terminal :size="13" /><span>轨迹来自 t_event；delta 不持久化，不会出现在这里。Ctrl/Cmd + 点击 ID 可复制。</span></div>
    </template>
  </div>
</template>
