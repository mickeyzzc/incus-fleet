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
    path: '/clusters/:cluster/members/:member',
    name: 'node-detail',
    component: () => import('@/views/NodeDetailView.vue'),
    props: true,
    meta: { titleKey: 'titles.node' },
  },
  {
    path: '/instances',
    name: 'instances',
    component: () => import('@/views/InstancesView.vue'),
    meta: { titleKey: 'titles.instances' },
  },
  {
    // Migration history is a record of instance operations, not a workspace.
    path: '/migrations',
    redirect: { path: '/instances', query: { view: 'migrations' } },
  },
  {
    path: '/assets',
    name: 'assets',
    component: () => import('@/views/AssetsView.vue'),
    meta: { titleKey: 'titles.assets' },
  },
  {
    // Answer profiles live with the other reusable automation content.
    path: '/answer-profiles',
    redirect: { path: '/assets', query: { tab: 'answers' } },
  },
  {
    path: '/volumes',
    name: 'fleet-volumes',
    component: () => import('@/views/FleetVolumesView.vue'),
    meta: { titleKey: 'titles.fleetVolumes' },
  },
  {
    path: '/plugins',
    name: 'plugins',
    component: () => import('@/views/PluginsView.vue'),
    meta: { titleKey: 'titles.plugins' },
  },
  {
    // The proxied plugin UI is framed inside fleet so the sidebar stays usable.
    path: '/plugin/:id',
    name: 'plugin-page',
    component: () => import('@/views/PluginPage.vue'),
    props: true,
    meta: { titleKey: 'titles.plugins' },
  },
  {
    path: '/users',
    name: 'users',
    component: () => import('@/views/UsersView.vue'),
    meta: { titleKey: 'titles.users', requiresAdmin: true },
  },
  {
    path: '/node',
    redirect: '/',
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

    if (to.meta.requiresAdmin === true && !auth.canAdmin) {
      return { name: 'dashboard' }
    }

    return true
  })

  // A tab left open across a deployment keeps the old entry bundle and asks
  // for hashed chunks that no longer exist. Reload once so the user gets the
  // current build instead of a dead menu item.
  router.onError((error) => {
    const message = error instanceof Error ? error.message : String(error)
    if (!/Failed to fetch dynamically imported module|Importing a module script failed/i.test(message)) {
      return
    }

    const key = 'incus-fleet.chunk-reload'
    if (sessionStorage.getItem(key)) {
      return
    }

    sessionStorage.setItem(key, String(Date.now()))
    window.location.reload()
  })

  router.afterEach((to) => {
    applyDocumentTitle(to)
  })

  return router
}

const router = createFleetRouter()

export default router
