import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/api'
import LoginView from './LoginView.vue'

const Blank = { template: '<div />' }

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'login', component: LoginView },
      { path: '/', name: 'dashboard', component: Blank },
      { path: '/instances', name: 'instances', component: Blank },
    ],
  })
}

async function mountLogin(path = '/login') {
  const router = createTestRouter()
  await router.push(path)
  await router.isReady()

  const wrapper = mount(LoginView, {
    global: { plugins: [ElementPlus, router] },
  })

  return { wrapper, router }
}

describe('LoginView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('logs in and redirects to the dashboard', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true })
    const { wrapper, router } = await mountLogin()

    await wrapper.find('input').setValue('secret')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.login).toHaveBeenCalledWith('secret')
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('honours a safe redirect query', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true })
    const { wrapper, router } = await mountLogin('/login?redirect=/instances')

    await wrapper.find('input').setValue('secret')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/instances')
  })

  it('ignores unsafe redirect queries', async () => {
    vi.mocked(api.login).mockResolvedValue({ authenticated: true })
    const { wrapper, router } = await mountLogin('/login?redirect=https://evil.example')

    await wrapper.find('input').setValue('secret')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/')
  })

  it('does not submit an empty token', async () => {
    const { wrapper } = await mountLogin()

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.login).not.toHaveBeenCalled()
  })

  it('shows invalid-token and generic errors', async () => {
    vi.mocked(api.login).mockRejectedValueOnce(new ApiError(401, 'invalid_token', 'bad token'))
    const { wrapper } = await mountLogin()

    await wrapper.find('input').setValue('wrong')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Invalid admin token.')

    vi.mocked(api.login).mockRejectedValueOnce(new Error('network down'))
    await wrapper.find('input').setValue('secret')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('network down')
  })
})
