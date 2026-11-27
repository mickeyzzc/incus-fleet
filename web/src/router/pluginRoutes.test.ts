import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'

import type { PluginNavEntry } from '@/plugins/model'
import { routeTitle, registerPluginRoutes } from './index'

const Blank = { template: '<div />' }

function testRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'dashboard', component: Blank },
      { path: '/plugins', name: 'plugins', component: Blank },
      { path: '/:pathMatch(.*)*', name: 'not-found', redirect: '/' },
    ],
  })
}

function entry(id: string): PluginNavEntry {
  return {
    id,
    path: `/plugins/${id}`,
    titleKey: `plugins.${id}`,
    title: id,
    icon: 'metrics',
    requiresRole: 'viewer',
  }
}

describe('data-driven plugin routes', () => {
  it('registers one route per installed plugin without duplicating them', () => {
    const router = testRouter()
    const count = registerPluginRoutes(router, [entry('vm'), entry('logs')])

    expect(count).toBe(2)
    expect(router.getRoutes().filter((route) => String(route.name).startsWith('plugin-ui-'))).toHaveLength(2)

    registerPluginRoutes(router, [entry('vm')])
    const names = router.getRoutes()
      .filter((route) => String(route.name).startsWith('plugin-ui-'))
      .map((route) => route.name)
    expect(names).toEqual(['plugin-ui-vm'])
  })

  it('keeps the catch-all last so a plugin path wins over it', async () => {
    const router = testRouter()
    registerPluginRoutes(router, [entry('vm')])

    await router.push('/plugins/vm')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('plugin-ui-vm')
    expect(router.currentRoute.value.meta.pluginId).toBe('vm')
    expect(router.currentRoute.value.matched.at(-1)?.components?.default).toBeTruthy()
  })

  it('still redirects unknown paths after re-registering routes', async () => {
    const router = testRouter()
    registerPluginRoutes(router, [entry('vm')])

    await router.push('/unknown')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/')
  })

  it('falls back to a plain title before a plugin message is merged', () => {
    expect(routeTitle({ meta: { titleKey: 'plugins.not-merged', title: 'Plain title' } } as any))
      .toBe('Plain title')
  })
})
