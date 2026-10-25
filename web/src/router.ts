import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AppShell from '@/components/AppShell.vue'
import LoginView from '@/views/LoginView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginView, meta: { title: '登录' } },
    {
      path: '/',
      component: AppShell,
      meta: { requiresAuth: true },
      children: [
        { path: '', name: 'dashboard', component: () => import('@/views/DashboardView.vue'), meta: { title: '总览' } },
        { path: 'users', name: 'users', component: () => import('@/views/UsersView.vue'), meta: { title: '用户管理' } },
        { path: 'users/:id', name: 'user-detail', component: () => import('@/views/UserDetailView.vue'), meta: { title: '用户详情' } },
        { path: 'models', name: 'models', component: () => import('@/views/ModelsView.vue'), meta: { title: '模型与用量' } },
        { path: 'usage', name: 'usage', component: () => import('@/views/UsageView.vue'), meta: { title: '用量分析' } },
        { path: 'presence', name: 'presence', component: () => import('@/views/PresenceView.vue'), meta: { title: '活跃用户' } },
        { path: 'runs', name: 'runs', component: () => import('@/views/RunsView.vue'), meta: { title: 'Agent 任务' } },
        { path: 'runs/:id', name: 'run-detail', component: () => import('@/views/RunDetailView.vue'), meta: { title: '执行轨迹' } },
        { path: 'audit', name: 'audit', component: () => import('@/views/AuditView.vue'), meta: { title: '审计日志' } },
        { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { title: '设置' } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (to.name === 'login') {
    const me = await auth.ensure()
    return me ? { path: '/' } : true
  }
  if (to.matched.some((r) => r.meta.requiresAuth)) {
    const me = await auth.ensure()
    if (!me) return { path: '/login', query: { redirect: to.fullPath } }
  }
  return true
})

router.afterEach((to) => {
  const title = (to.meta.title as string | undefined) || 'Tenon Ops'
  document.title = `${title} · Tenon Ops`
})

export default router
