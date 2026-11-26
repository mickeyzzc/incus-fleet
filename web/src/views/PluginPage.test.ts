import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'

import PluginPage from './PluginPage.vue'

async function mountPage(id: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/plugin/:id', component: PluginPage, props: true }],
  })
  await router.push(`/plugin/${id}`)
  await router.isReady()

  return mount(PluginPage, { global: { plugins: [router, ElementPlus] } })
}

describe('PluginPage', () => {
  it('frames the same-origin proxy for the requested plugin', async () => {
    const wrapper = await mountPage('vmlogs')
    const frame = wrapper.find('iframe')

    expect(frame.exists()).toBe(true)
    expect(frame.attributes('src')).toBe('/plugin-ui/vmlogs/')
    expect(wrapper.text()).toContain('vmlogs')
  })
})
