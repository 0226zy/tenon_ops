export function fmtTime(iso?: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export function fmtDate(iso?: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString('zh-CN')
}

export function timeAgo(iso?: string | null): string {
  if (!iso) return '—'
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return iso
  const diff = Date.now() - t
  if (diff < 0) return '刚刚'
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s} 秒前`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} 分钟前`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时前`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d} 天前`
  return fmtDate(iso)
}

export function fmtNumber(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—'
  if (Math.abs(v) >= 1_000_000) return `${(v / 1_000_000).toFixed(2)}M`
  if (Math.abs(v) >= 1_000) return `${(v / 1_000).toFixed(1)}k`
  return String(Math.round(v))
}

export function fmtInt(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—'
  return new Intl.NumberFormat('zh-CN').format(v)
}

export function fmtCost(v: number | null | undefined, currency = 'CNY'): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—'
  return `¥${v.toFixed(v < 1 ? 4 : 2)}`
}

export function fmtDuration(ms: number | null | undefined): string {
  if (!ms || ms < 0) return '—'
  if (ms < 1000) return `${ms}ms`
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}

export function fmtSeconds(v: number | null | undefined): string {
  if (v === null || v === undefined) return '—'
  return fmtDuration(v * 1000)
}

export function runTone(status: string): string {
  switch (status) {
    case 'finished': return 'green'
    case 'failed': return 'red'
    case 'cancelled': return 'gray'
    case 'running': return 'blue'
    case 'queued': case 'dispatching': return 'amber'
    case 'cancelling': case 'finalizing': return 'violet'
    default: return 'gray'
  }
}

export function runLabel(r: { status_text: string; active: boolean }): string {
  const labels: Record<string, string> = {
    queued: '排队中', dispatching: '调度中', running: '执行中', cancelling: '取消中',
    finalizing: '收尾中', finished: '已完成', failed: '失败', cancelled: '已取消',
  }
  return labels[r.status_text] || r.status_text
}

export function userTone(status: number): string {
  return status === 1 ? 'green' : 'gray'
}

export function pwdTone(state: string): string {
  if (state === 'managed') return 'green'
  if (state === 'delivered_removed') return 'blue'
  return 'amber'
}

export function pwdLabel(state: string): string {
  if (state === 'managed') return '可查看'
  if (state === 'delivered_removed') return '交付后清除'
  return '历史未知'
}

export function tierTone(tier: string): string {
  switch (tier) {
    case 'running': return 'violet'
    case 'online': return 'green'
    case 'active': return 'blue'
    case 'recent': return 'amber'
    case 'today': return 'gray'
    default: return 'gray'
  }
}

export function tierLabel(tier: string): string {
  const labels: Record<string, string> = {
    running: '运行中', online: '在线', active: '活跃', recent: '最近', today: '今日', idle: '未活跃',
  }
  return labels[tier] || tier
}

export function jsonPretty(v: unknown): string {
  try { return JSON.stringify(v, null, 2) } catch { return String(v) }
}

export function shortID(v: string, n = 10): string {
  if (!v) return '—'
  return v.length > n ? `${v.slice(0, n)}…` : v
}
