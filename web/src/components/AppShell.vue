<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity, Boxes, ChartNoAxesCombined, LayoutDashboard, ListTree, LogOut, Menu,
  ScrollText, Settings, ShieldCheck, Users, X,
} from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const toast = useToastStore()
const open = ref(false)

const nav = [
  { to: '/', label: '总览', icon: LayoutDashboard, exact: true },
  { to: '/users', label: '用户管理', icon: Users },
  { to: '/presence', label: '活跃用户', icon: Activity },
  { to: '/models', label: '模型目录', icon: Boxes },
  { to: '/usage', label: '用量分析', icon: ChartNoAxesCombined },
  { to: '/runs', label: 'Agent 任务', icon: ListTree },
  { to: '/audit', label: '审计日志', icon: ScrollText },
  { to: '/settings', label: '设置', icon: Settings },
]
const title = computed(() => (route.meta.title as string | undefined) || 'Tenon Ops')
const initials = computed(() => (auth.me?.display_name || auth.me?.admin_id || 'A').slice(0, 1).toUpperCase())
const isLogin = computed(() => route.name === 'login')

watch(() => route.fullPath, () => { open.value = false })

async function logout() {
  await auth.logout()
  toast.success('已退出登录')
  await router.push('/login')
}
</script>

<template>
  <div v-if="!isLogin" class="shell">
    <aside class="sidebar" :class="{ open }">
      <div class="brand">
        <div class="brand-mark">T</div>
        <div>
          <div class="brand-name">Tenon Ops</div>
          <div class="brand-sub">operations console</div>
        </div>
        <button class="mobile-toggle" style="margin-left:auto;color:#94a3b8" aria-label="关闭菜单" @click="open = false">
          <X :size="18" />
        </button>
      </div>

      <nav class="nav">
        <div class="nav-label">Workspace</div>
        <RouterLink v-for="item in nav" :key="item.to" :to="item.to" class="nav-link" :class="{ 'router-link-exact-active': item.exact && route.path === '/' }">
          <component :is="item.icon" />
          <span>{{ item.label }}</span>
        </RouterLink>
      </nav>

      <div class="sidebar-foot">
        <div class="env-chip"><span class="env-dot" />{{ auth.environment }}</div>
        <div class="small" style="color:#64748b;display:flex;align-items:center;gap:5px">
          <ShieldCheck :size="12" /> identity writable · telemetry read-only
        </div>
        <div class="small" style="color:#475569">v{{ auth.version }}</div>
      </div>
    </aside>

    <div class="main">
      <header class="topbar">
        <button class="mobile-toggle" aria-label="打开菜单" @click="open = true"><Menu :size="20" /></button>
        <div class="crumb"><span>Tenon Ops</span><span style="color:#cbd5e1">/</span><strong>{{ title }}</strong></div>
        <div class="top-actions">
          <span class="env-pill"><span class="dot" />{{ auth.environment }}</span>
          <div class="user-menu">
            <span class="avatar">{{ initials }}</span>
            <div style="line-height:1.2">
              <div style="font-size:12px;font-weight:600">{{ auth.me?.display_name || auth.me?.admin_id }}</div>
              <div style="font-size:10px;color:var(--muted)">{{ auth.me?.role }}</div>
            </div>
          </div>
          <button class="btn btn-icon" title="退出登录" @click="logout"><LogOut :size="15" /></button>
        </div>
      </header>
      <main class="content">
        <RouterView />
      </main>
    </div>
  </div>
  <RouterView v-else />
</template>
