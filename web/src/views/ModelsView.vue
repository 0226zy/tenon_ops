<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  AlertTriangle, Boxes, Check, CircleHelp, Coins, FileUp, Loader2, Pencil, Plus, RefreshCw, ToggleLeft, ToggleRight,
} from 'lucide-vue-next'
import PageHeader from '@/components/PageHeader.vue'
import Badge from '@/components/Badge.vue'
import Modal from '@/components/Modal.vue'
import EmptyState from '@/components/EmptyState.vue'
import { api, ApiError } from '@/api/client'
import type { ModelView } from '@/api/types'
import { fmtCost, fmtNumber, fmtTime } from '@/utils/format'
import { useToastStore } from '@/stores/toast'

const toast = useToastStore()
const items = ref<ModelView[]>([])
const loading = ref(true)
const error = ref('')

const importOpen = ref(false)
const importBusy = ref(false)
const importError = ref('')
const yamlText = ref('')
const importResult = ref<{ imported: number; models: string[] } | null>(null)

const priceTarget = ref<ModelView | null>(null)
const priceOpen = ref(false)
const priceBusy = ref(false)
const priceError = ref('')
const price = reactive({ effective_from: '', input_per_mtok: 0, output_per_mtok: 0, currency: 'CNY' })

const editTarget = ref<ModelView | null>(null)
const editOpen = ref(false)
const editBusy = ref(false)
const editError = ref('')
const edit = reactive({ display_name: '', provider: '', notes: '' })

const enabledCount = computed(() => items.value.filter((m) => m.enabled).length)
const pricedCount = computed(() => items.value.filter((m) => !!m.price).length)

async function load() {
  loading.value = true
  error.value = ''
  try { items.value = (await api.models.list()).items }
  catch (e) { error.value = e instanceof ApiError ? e.message : '加载模型失败' }
  finally { loading.value = false }
}

function openImport() {
  yamlText.value = ''
  importError.value = ''
  importResult.value = null
  importOpen.value = true
}

async function onFile(ev: Event) {
  const file = (ev.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (file.size > 1 << 20) { importError.value = '文件不能超过 1MB'; return }
  yamlText.value = await file.text()
}

async function submitImport() {
  if (importBusy.value) return
  importBusy.value = true
  importError.value = ''
  try {
    importResult.value = await api.models.import(yamlText.value)
    toast.success(`已导入 ${importResult.value.imported} 个模型`)
    await load()
  } catch (e) { importError.value = e instanceof ApiError ? e.message : '导入失败' }
  finally { importBusy.value = false }
}

function openPrice(m: ModelView) {
  priceTarget.value = m
  price.effective_from = new Date().toISOString().slice(0, 16)
  price.input_per_mtok = m.price?.input_per_mtok ?? 0
  price.output_per_mtok = m.price?.output_per_mtok ?? 0
  price.currency = m.price?.currency ?? 'CNY'
  priceError.value = ''
  priceOpen.value = true
}

async function submitPrice() {
  if (!priceTarget.value || priceBusy.value) return
  priceBusy.value = true
  priceError.value = ''
  try {
    await api.models.addPrice(priceTarget.value.model_id, {
      effective_from: price.effective_from ? new Date(price.effective_from).toISOString() : undefined,
      input_per_mtok: Number(price.input_per_mtok), output_per_mtok: Number(price.output_per_mtok), currency: price.currency,
    })
    toast.success('价格已生效')
    priceOpen.value = false
    await load()
  } catch (e) { priceError.value = e instanceof ApiError ? e.message : '保存价格失败' }
  finally { priceBusy.value = false }
}

function openEdit(m: ModelView) {
  editTarget.value = m
  edit.display_name = m.display_name
  edit.provider = m.provider
  edit.notes = m.notes
  editError.value = ''
  editOpen.value = true
}

async function submitEdit() {
  if (!editTarget.value || editBusy.value) return
  editBusy.value = true
  editError.value = ''
  try {
    await api.models.patch(editTarget.value.model_id, { display_name: edit.display_name.trim(), provider: edit.provider.trim(), notes: edit.notes.trim() })
    toast.success('模型元数据已更新')
    editOpen.value = false
    await load()
  } catch (e) { editError.value = e instanceof ApiError ? e.message : '保存失败' }
  finally { editBusy.value = false }
}

async function toggle(m: ModelView) {
  try {
    await api.models.patch(m.model_id, { enabled: !m.enabled })
    toast.success(m.enabled ? '模型已停用（仅管理端镜像）' : '模型已启用（仅管理端镜像）')
    await load()
  } catch (e) { toast.error(e instanceof ApiError ? e.message : '操作失败') }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="模型目录" subtitle="目录镜像用于展示、计价与关联用量；不改变 Tenon 的实际路由，密钥永远不在管理端。">
      <template #actions>
        <button class="btn" :disabled="loading" @click="load"><RefreshCw :size="14" :class="{ spin: loading }" /> 刷新</button>
        <button class="btn btn-primary" @click="openImport"><FileUp :size="14" /> 导入模型</button>
      </template>
    </PageHeader>

    <div class="banner info" style="margin-bottom:14px">
      <CircleHelp :size="15" />
      <span>就绪状态为“未知”：control 的 <span class="mono">api_key_env</span> 只在 Tenon 进程里，管理端不猜。P1 接入控制层只读接口后显示 <span class="mono">ready/missing_env</span>。</span>
    </div>
    <div v-if="error" class="banner danger" style="margin-bottom:14px"><AlertTriangle :size="15" /> {{ error }}<button class="link-btn" style="margin-left:auto" @click="load">重试</button></div>

    <div class="grid-3" style="margin-bottom:16px">
      <div class="card stat"><div class="stat-label"><Boxes :size="13" /> 模型总数</div><div class="stat-value">{{ items.length }}</div><div class="stat-meta">{{ enabledCount }} 个启用</div></div>
      <div class="card stat"><div class="stat-label"><Coins :size="13" /> 已定价模型</div><div class="stat-value">{{ pricedCount }}</div><div class="stat-meta">未定价模型成本显示为“未定价”</div></div>
      <div class="card stat"><div class="stat-label"><Check :size="13" /> 默认模型</div><div class="stat-value">{{ items.filter((m) => m.is_default).map((m) => m.model_id).join(', ') || '—' }}</div><div class="stat-meta">仅展示目录标记，不代表 control 当前默认</div></div>
    </div>

    <section class="card">
      <div v-if="loading" class="table-empty"><Loader2 :size="22" class="spin" /><div>加载模型目录…</div></div>
      <div v-else-if="!items.length" class="table-empty"><EmptyState title="还没有模型" text="导入 Tenon 的 models.yaml，或手工创建目录镜像。"><template #action><button class="btn btn-primary" @click="openImport"><Plus :size="14" /> 导入模型</button></template></EmptyState></div>
      <div v-else class="table-wrap">
        <table class="table">
          <thead><tr><th>模型</th><th>来源</th><th>就绪</th><th class="right">24h 请求</th><th class="right">24h Token</th><th class="right">24h 成本</th><th>价格 / 1M</th><th>同步</th><th style="width:170px"></th></tr></thead>
          <tbody>
            <tr v-for="m in items" :key="m.model_id">
              <td><div class="row"><span class="mono" style="font-weight:600">{{ m.model_id }}</span><Badge v-if="m.is_default" tone="blue">default</Badge><Badge v-if="!m.enabled" tone="gray">停用</Badge></div><div class="small muted">{{ m.display_name }}<span v-if="m.base_url"> · {{ m.base_url }}</span></div></td>
              <td><Badge tone="gray">{{ m.source }}</Badge><div class="small muted" style="margin-top:3px">{{ m.api_key_env || 'no key env' }}</div></td>
              <td><Badge tone="amber" dot>未知</Badge><div class="small muted" style="margin-top:3px">未接控制层</div></td>
              <td class="right">{{ fmtNumber(m.requests_24h) }}</td>
              <td class="right mono">{{ fmtNumber(m.tokens_24h) }}</td>
              <td class="right">{{ m.price ? fmtCost(m.cost_24h) : '未定价' }}</td>
              <td><span v-if="m.price" class="small mono">in {{ m.price.input_per_mtok }} / out {{ m.price.output_per_mtok }} {{ m.price.currency }}</span><span v-else class="small muted">—</span></td>
              <td class="small muted">{{ fmtTime(m.synced_at || m.modified_at) }}</td>
              <td><div class="actions">
                <button class="btn btn-sm" title="定价" @click="openPrice(m)"><Coins :size="13" /></button>
                <button class="btn btn-sm" title="编辑元数据" @click="openEdit(m)"><Pencil :size="13" /></button>
                <button class="btn btn-sm" :title="m.enabled ? '停用镜像' : '启用镜像'" @click="toggle(m)"><ToggleRight v-if="m.enabled" :size="13" /><ToggleLeft v-else :size="13" /></button>
              </div></td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <Modal :open="importOpen" title="导入模型目录" wide :closable="!importBusy" @close="importOpen = false">
      <div v-if="!importResult" class="stack">
        <div class="banner info"><FileUp :size="15" /><span>严格校验 <span class="mono">id/name/base_url/api_key_env/upstream_model/max_tokens</span> 等字段；不导入任何密钥，只保留环境变量名。</span></div>
        <div class="row"><input type="file" accept=".yaml,.yml,text/yaml" class="input" style="padding:6px" @change="onFile" /><span class="small muted">或直接粘贴</span></div>
        <textarea v-model="yamlText" class="textarea input-mono" style="min-height:260px" placeholder="models:&#10;  - id: deepseek&#10;    name: DeepSeek&#10;    protocol: openai&#10;    base_url: https://api.deepseek.com/v1&#10;    upstream_model: deepseek-chat&#10;    api_key_env: DEEPSEEK_API_KEY&#10;    default: true" />
        <div v-if="importError" class="error-text">{{ importError }}</div>
        <div class="hint">导入是替换式镜像：本次未出现的旧模型会被标记为停用，不会删除历史用量关联。</div>
      </div>
      <div v-else class="stack"><div class="banner info"><Check :size="15" /> 已导入 {{ importResult.imported }} 个模型。</div><div class="row wrap"><span v-for="m in importResult.models" :key="m" class="tag mono">{{ m }}</span></div></div>
      <template #footer><button class="btn" @click="importOpen = false">{{ importResult ? '关闭' : '取消' }}</button><button v-if="!importResult" class="btn btn-primary" :disabled="importBusy || !yamlText.trim()" @click="submitImport"><Loader2 v-if="importBusy" :size="14" class="spin" /> 解析并导入</button></template>
    </Modal>

    <Modal :open="priceOpen" title="设置模型价格" :closable="!priceBusy" @close="priceOpen = false">
      <div class="stack">
        <div class="row"><span class="mono" style="font-weight:600">{{ priceTarget?.model_id }}</span><Badge tone="gray">每 1M tokens</Badge></div>
        <div class="field"><label class="label">生效时间</label><input v-model="price.effective_from" class="input" type="datetime-local" /></div>
        <div class="form-row"><div class="field"><label class="label">输入价格</label><input v-model.number="price.input_per_mtok" class="input" type="number" min="0" step="0.000001" /></div><div class="field"><label class="label">输出价格</label><input v-model.number="price.output_per_mtok" class="input" type="number" min="0" step="0.000001" /></div></div>
        <div class="field"><label class="label">币种</label><select v-model="price.currency" class="select"><option>CNY</option><option>USD</option><option>EUR</option></select></div>
        <div class="hint">价格按生效时间保留历史；已发生用量不会自动重算。</div>
        <div v-if="priceError" class="error-text">{{ priceError }}</div>
      </div>
      <template #footer><button class="btn" :disabled="priceBusy" @click="priceOpen = false">取消</button><button class="btn btn-primary" :disabled="priceBusy" @click="submitPrice"><Loader2 v-if="priceBusy" :size="14" class="spin" /> 保存价格</button></template>
    </Modal>

    <Modal :open="editOpen" title="编辑模型元数据" :closable="!editBusy" @close="editOpen = false">
      <div class="stack"><div class="field"><label class="label">模型 ID</label><input class="input input-mono" :value="editTarget?.model_id" disabled /></div><div class="form-row"><div class="field"><label class="label">显示名</label><input v-model="edit.display_name" class="input" /></div><div class="field"><label class="label">供应商</label><input v-model="edit.provider" class="input" /></div></div><div class="field"><label class="label">备注</label><textarea v-model="edit.notes" class="textarea" maxlength="512" /></div><div v-if="editError" class="error-text">{{ editError }}</div></div>
      <template #footer><button class="btn" :disabled="editBusy" @click="editOpen = false">取消</button><button class="btn btn-primary" :disabled="editBusy" @click="submitEdit"><Loader2 v-if="editBusy" :size="14" class="spin" /> 保存</button></template>
    </Modal>
  </div>
</template>
