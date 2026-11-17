import {
  createRouter,
  createWebHistory,
  type RouteLocationNormalized,
  type RouteRecordRaw,
  type Router,
  type RouterHistory,
} from 'vue-router'

import i18n from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { public: true, titleKey: 'titles.signIn' },
  },
  {
    path: '/',
    name: 'dashboard',
    component: () => import('@/views/DashboardView.vue'),
    meta: { titleKey: 'titles.dashboard' },
  },
  {
    path: '/clusters/:cluster',
    name: 'cluster',
    component: () => import('@/views/ClusterView.vue'),
    props: true,
    meta: { titleKey: 'titles.cluster' },
  },
  {
    path: '/instances',
    name: 'instances',
    component: () => import('@/views/InstancesView.vue'),
    meta: { titleKey: 'titles.instances' },
  },
  {
    path: '/migrations',
    name: 'migrations',
    component: () => import('@/views/MigrationsView.vue'),
    meta: { titleKey: 'titles.migrations' },
  },
  {
    path: '/assets',
    name: 'assets',
    component: () => import('@/views/AssetsView.vue'),
    meta: { titleKey: 'titles.assets' },
  },
  {
    path: '/node',
    name: 'node',
    component: () => import('@/views/NodeView.vue'),
    meta: { titleKey: 'titles.node' },
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/',
  },
]

/** Resolves the translated page title for a route. */
export function routeTitle(to: RouteLocationNormalized): string {
  if (typeof to.meta.titleKey === 'string') {
    return i18n.global.t(to.meta.titleKey)
  }
  if (typeof to.meta.title === 'string') {
    return to.meta.title
  }

  return i18n.global.t('app.brand')
}

/** Applies the translated document title for a route. */
export function applyDocumentTitle(to: RouteLocationNormalized): void {
  document.title = `${routeTitle(to)} · ${i18n.global.t('app.brand')}`
}

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
    applyDocumentTitle(to)
  })

  return router
}

const router = createFleetRouter()

export default router
