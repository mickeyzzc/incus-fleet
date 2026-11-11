import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { describe, expect, it } from 'vitest'

import MigrationStatusTag from './MigrationStatusTag.vue'
import StatusTag from './StatusTag.vue'
import StorageBar from './StorageBar.vue'
import { ElProgress } from 'element-plus'

function mountWithElement(component: Parameters<typeof mount>[0], options: Record<string, unknown> = {}) {
  return mount(component, {
    ...options,
    global: {
      plugins: [ElementPlus],
    },
  })
}

describe('StatusTag', () => {
  it.each([
    ['Running', 'success'],
    ['frozen', 'warning'],
    ['Starting', 'warning'],
    ['Error', 'danger'],
    ['Stopped', 'info'],
    ['Shutdown', 'info'],
    ['Unknown', 'primary'],
    ['created', 'primary'],
  ])('maps %s to a %s tag', (status, expected) => {
    const wrapper = mountWithElement(StatusTag, { props: { status } })

    expect(wrapper.text()).toBe(status.charAt(0).toUpperCase() + status.slice(1))
    expect(wrapper.find('.el-tag').classes()).toContain(`el-tag--${expected}`)
  })

  it('uses Unknown for empty statuses and accepts a status code', () => {
    const wrapper = mountWithElement(StatusTag, { props: { status: '', statusCode: 103 } })

    expect(wrapper.text()).toBe('Unknown')
  })
})

describe('MigrationStatusTag', () => {
  it.each([
    ['pending', 'Queued', 'info'],
    ['preflight', 'Preflight', 'info'],
    ['stopping', 'Stopping source', 'warning'],
    ['transferring', 'Transferring', 'primary'],
    ['starting', 'Starting', 'warning'],
    ['verifying', 'Verifying', 'warning'],
    ['done', 'Done', 'success'],
    ['failed', 'Failed', 'danger'],
    ['mystery', 'mystery', 'info'],
  ])('maps %s', (status, label, expected) => {
    const wrapper = mountWithElement(MigrationStatusTag, { props: { status } })

    expect(wrapper.text()).toBe(label)
    expect(wrapper.find('.el-tag').classes()).toContain(`el-tag--${expected}`)
  })
})

describe('StorageBar', () => {
  it('renders usage and a healthy progress bar', async () => {
    const wrapper = mountWithElement(StorageBar, {
      props: { used: 512, total: 1024, pool: 'default' },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Storage · default')
    expect(wrapper.text()).toContain('512 B / 1.0 KiB (50.0%)')
    expect(wrapper.findComponent(ElProgress).props('status')).toBe('success')
  })

  it('warns at 75 percent and errors at 90 percent', () => {
    const warning = mountWithElement(StorageBar, { props: { used: 80, total: 100 } })
    const error = mountWithElement(StorageBar, { props: { used: 95, total: 100 } })

    expect(warning.findComponent(ElProgress).props('status')).toBe('warning')
    expect(error.findComponent(ElProgress).props('status')).toBe('exception')
  })

  it('handles a zero total', () => {
    const wrapper = mountWithElement(StorageBar, { props: { used: 10, total: 0 } })

    expect(wrapper.text()).toContain('0%')
    expect(wrapper.findComponent(ElProgress).props('percentage')).toBe(0)
    expect(wrapper.findComponent(ElProgress).props('status')).toBe('success')
  })
})
