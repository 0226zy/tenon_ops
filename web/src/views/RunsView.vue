<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { AlertTriangle, ListTree, Loader2, RefreshCw } from 'lucide-vue-next'
import PageHeader from '@/components/PageHeader.vue'
import Badge from '@/components/Badge.vue'
import EmptyState from '@/components/EmptyState.vue'
import { api, ApiError } from '@/api/client'
import type { Run } from '@/api/types'
import { fmtDuration, fmtNumber, fmtTime, runLabel, runTone, shortID } from '@/utils/format'

const router = useRouter()
const items = ref<Run[]>([])
const next = ref('')
const loading = ref(true)
const error = ref('')
const status = ref('all')
const userId = ref('')
const model = ref('')
let timer: number | undefined

const hasActive = computed(() => items.value.some((r) => r.active))

async function load(silent = false, more = false) {
  if (!silent && !more) loading.value = true
  error.value = ''
  try {
    const page = await api.runs.list({
      status: status.value === 'all' ? undefined : status.value,
      user_id: userId.value.trim() || undefined, model: model.value.trim() || undefined,
      cursor: more ? next.value : undefined, limit: 50,
    })
    items.value = more ? [...items.value, ...page.items] : page.items
    next.value = page.next_cursor
  } catch (e) { error.value = e instanceof ApiError ? e.message : '加载任务失败' }
  finally { loading.value = false }
}

function schedule() {
  if (timer) window.clearInterval(timer)
  timer = window.setInterval(() => { if (!document.hidden && hasActive.value) void load(true) }, 5000)
}

watch(status, () => { void load(); schedule() })
onMounted(() => { void load(); schedule() })
onBeforeUnmount(() => { if (timer) window.clearInterval(timer) })
</script>

<template>
  <div>
    <PageHeader title="Agent 任务" subtitle="Run 是 Tenon 的 Agent 任务；轨迹来自 t_event，工具调用/结果按 call_id 配对。">
      <template #actions>
        <button class="btn" :disabled="loading" @click="load()"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button>
      </template>
    </PageHeader>

    <section class="card">
      <div class="table-toolbar">
        <div class="seg">
          <button v-for="s in [{k:'all',l:'全部'},{k:'active',l:'运行中'},{k:'failed',l:'失败'},{k:'finished',l:'完成'},{k:'cancelled',l:'取消'}]" :key="s.k" :class="{ active: status === s.k }" @click="status = s.k">{{ s.l }}</button>
        </div>
        <input v-model="userId" class="input" style="width:170px" placeholder="user_id" @keyup.enter="load()" />
        <input v-model="model" class="input" style="width:170px" placeholder="model" @keyup.enter="load()" />
        <button class="btn" @click="load()">筛选</button>
        <span class="spacer" />
        <span class="small muted">{{ items.length }} 个任务<span v-if="hasActive"> · 执行中自动刷新</span></span>
      </div>
      <div v-if="error" class="banner danger" style="margin:14px"><AlertTriangle :size="15" /> {{ error }}<button class="link-btn" style="margin-left:auto" @click="load()">重试</button></div>
      <div v-if="loading && !items.length" class="table-empty"><Loader2 :size="22" class="spin" /><div>加载任务…</div></div>
      <div v-else-if="!items.length" class="table-empty"><EmptyState title="没有匹配的任务" text="调整筛选条件，或等待 Tenon 产生 Run。" /></div>
      <div v-else class="table-wrap">
        <table class="table">
          <thead><tr><th>Run</th><th>用户</th><th>对话</th><th>模型</th><th>状态</th><th class="right">迭代</th><th class="right">Token</th><th class="right">耗时</th><th>创建</th><th></th></tr></thead>
          <tbody><tr v-for="r in items" :key="r.run_id" class="clickable" @click="router.push(`/runs/${encodeURIComponent(r.run_id)}`)">
            <td><span class="mono" style="font-weight:600">{{ shortID(r.run_id, 14) }}</span><div v-if="r.error_code" class="small" style="color:var(--danger)">{{ r.error_code }}</div></td>
            <td><RouterLink :to="`/users/${encodeURIComponent(r.user_id)}`" class="small" @click.stop>{{ r.user_name || r.user_id }}</RouterLink></td>
            <td><span class="small truncate" style="display:block;max-width:220px">{{ r.conv_title || r.conv_id }}</span></td>
            <td class="mono small">{{ r.model }}</td>
            <td><Badge :tone="runTone(r.status_text)" dot>{{ runLabel(r) }}</Badge></td>
            <td class="right">{{ r.iterations || '—' }}</td>
            <td class="right mono">{{ fmtNumber(r.total_tokens) }}</td>
            <td class="right small muted">{{ fmtDuration(r.duration_ms) }}</td>
            <td class="small muted">{{ fmtTime(r.created_at) }}</td>
            <td><span class="small muted">详情 →</span></td>
          </tr></tbody>
        </table>
      </div>
      <div v-if="next" class="table-foot"><span>继续加载更早的任务</span><button class="btn btn-sm" @click="load(false, true)">加载更多</button></div>
      <div v-else class="table-foot"><span><ListTree :size="12" style="display:inline" /> 轨迹详情在任务页</span><span>数据来自 t_run / t_event</span></div>
    </section>
  </div>
</template>
