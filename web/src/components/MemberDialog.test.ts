import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api, ApiError } from '@/api'
import { clusterSummary } from '@/test/fixtures'
import { useClustersStore } from '@/stores/clusters'
import MemberDialog from './MemberDialog.vue'

import type { ClusterMember } from '@/api/types'

function member(overrides: Partial<ClusterMember> = {}): ClusterMember {
  return {
    name: 'node-1',
    url: 'https://192.0.2.10:8443',
    status: 'Online',
    message: 'Fully operational',
    database: true,
    roles: ['database-leader'],
    architecture: 'x86_64',
    description: 'primary node',
    failure_domain: 'rack-a',
    groups: ['rack-a'],
    ...overrides,
  }
}

function mountDialog(memberValue: ClusterMember | null = member()) {
  return mount(MemberDialog, {
    props: { modelValue: true, cluster: 'c1', member: memberValue },
    global: { plugins: [ElementPlus] },
  })
}

describe('MemberDialog', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(api.listClusters).mockResolvedValue([clusterSummary()])
    vi.mocked(api.updateMember).mockResolvedValue(undefined)
  })

  it('prefills and submits member metadata', async () => {
    const store = useClustersStore()
    await store.fetchAll()
    const wrapper = mountDialog()
    await flushPromises()

    const inputs = wrapper.findAll('input')
    expect((inputs[0]?.element as HTMLInputElement | undefined)?.value).toBe('primary node')
    expect((inputs[1]?.element as HTMLInputElement | undefined)?.value).toBe('rack-a')
    expect((inputs[2]?.element as HTMLInputElement | undefined)?.value).toBe('rack-a')

    await inputs[0]?.setValue('primary node, nvme pool')
    await inputs[2]?.setValue('rack-a, gpu')

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateMember).toHaveBeenCalledWith('c1', 'node-1', {
      description: 'primary node, nvme pool',
      failure_domain: 'rack-a',
      groups: ['rack-a', 'gpu'],
    })
    expect(ElMessage.success).toHaveBeenCalledWith('Member node-1 updated.')
    expect(wrapper.emitted('saved')).toEqual([['node-1']])
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })

  it('omits empty optional fields', async () => {
    const wrapper = mountDialog(member({ description: '', failure_domain: '', groups: [] }))
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateMember).toHaveBeenCalledWith('c1', 'node-1', {
      description: '',
    })
  })

  it('reports failures from the API', async () => {
    vi.mocked(api.updateMember).mockRejectedValue(new ApiError(400, 'invalid_request', 'nope'))
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(ElMessage.error).toHaveBeenCalledWith('The request was rejected. Check the values and try again.')
  })

  it('does nothing without a member', async () => {
    const wrapper = mountDialog(null)
    await flushPromises()

    await wrapper.findAll('button').at(-1)?.trigger('click')
    await flushPromises()

    expect(api.updateMember).not.toHaveBeenCalled()
  })

  it('closes via the visible setter', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    wrapper.findComponent({ name: 'ElDialog' }).vm.$emit('update:modelValue', false)
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([false])
  })
})
