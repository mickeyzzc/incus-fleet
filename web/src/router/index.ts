import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
  type Router,
  type RouterHistory,
} from 'vue-router'

import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { public: true, title: 'Sign in' },
  },
  {
    path: '/',
    name: 'dashboard',
    component: () => import('@/views/DashboardView.vue'),
    meta: { title: 'Dashboard' },
  },
  {
    path: '/clusters/:cluster',
    name: 'cluster',
    component: () => import('@/views/ClusterView.vue'),
    props: true,
    meta: { title: 'Cluster' },
  },
  {
    path: '/instances',
    name: 'instances',
    component: () => import('@/views/InstancesView.vue'),
    meta: { title: 'Instances' },
  },
  {
    path: '/migrations',
    name: 'migrations',
    component: () => import('@/views/MigrationsView.vue'),
    meta: { title: 'Migrations' },
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/',
  },
]

/** Builds a fleet router. Tests pass a memory history. */
export function createFleetRouter(history: RouterHistory = createWebHistory()): Router {
  const router = createRouter({
    history,
    routes,
    scrollBehavior: () => ({ top: 0 }),
  })

  router.beforeEach(async (to) => {
    const auth = useAuthStore()

    if (!auth.initialized) {
      await auth.initialize()
    }

    if (to.meta.public === true) {
      if (auth.authenticated && to.name === 'login') {
        return { name: 'dashboard' }
      }

      return true
    }

    if (!auth.authenticated) {
      return {
        name: 'login',
        query: to.fullPath === '/' ? {} : { redirect: to.fullPath },
      }
    }

    return true
  })

  router.afterEach((to) => {
    const title = typeof to.meta.title === 'string' ? to.meta.title : 'Incus Fleet'
    document.title = `${title} · Incus Fleet`
  })

  return router
}

const router = createFleetRouter()

export default router
