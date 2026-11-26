import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { nextTick } from 'vue'

import HostMetricsPanel from '@/components/HostMetricsPanel.vue'
import { api } from '@/api'

describe('HostMetricsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(api.hostMetrics as any).mockResolvedValue([
      {
        name: 'fleet_host_cpu_usage_ratio',
        labels: {},
        type: 'gauge',
        points: [{ at: '2026-11-18T00:00:00Z', value: 0.25 }],
      },
      {
        name: 'fleet_host_memory_bytes',
        labels: { state: 'total' },
        type: 'gauge',
        points: [{ at: '2026-11-18T00:00:00Z', value: 1024 }],
      },
      {
        name: 'fleet_host_memory_bytes',
        labels: { state: 'used' },
        type: 'gauge',
        points: [{ at: '2026-11-18T00:00:00Z', value: 512 }],
      },
      {
        name: 'fleet_host_load',
        labels: { window: '1' },
        type: 'gauge',
        points: [{ at: '2026-11-18T00:00:00Z', value: 0.75 }],
      },
    ])
  })

  it('renders the latest host metrics', async () => {
    const wrapper = mount(HostMetricsPanel, {
      props: { cluster: 'c1', member: 'node-a' },
      global: {
        stubs: {
          Sparkline: true,
          ElButton: true,
          ElProgress: true,
          ElAlert: true,
        },
      },
    })
    await nextTick()
    await nextTick()
    expect(api.hostMetrics).toHaveBeenCalledWith('c1', 'node-a')
    expect(wrapper.text()).toContain('Host metrics')
    expect(wrapper.text()).toContain('25.0%')
    expect(wrapper.text()).toContain('0.75')
  })

  it('switches to the external Prometheus data source', async () => {
    ;(api.metricQueryRange as any).mockResolvedValue([
      {
        name: 'fleet_host_load',
        labels: { cluster: 'c1', member: 'node-a', window: '1' },
        type: 'gauge',
        points: [{ at: '2026-11-18T00:00:00Z', value: 2 }],
      },
    ])
    const wrapper = mount(HostMetricsPanel, {
      props: { cluster: 'c1', member: 'node-a' },
      global: { stubs: { Sparkline: true, ElButton: true, ElProgress: true, ElAlert: true } },
    })
    await nextTick()
    await wrapper.find('select').setValue('external')
    await nextTick()
    await nextTick()
    expect(api.metricQueryRange).toHaveBeenCalled()
    expect(wrapper.text()).toContain('2.00')
  })

  it('automatically falls back to the external source when center telemetry is off', async () => {
    vi.mocked(api.hostMetrics).mockRejectedValueOnce(new Error('telemetry is not enabled'))
    vi.mocked(api.metricQueryRange).mockResolvedValue([
      {
        name: 'fleet_host_load',
        labels: { cluster: 'c1', member: 'node-a', window: '1' },
        type: 'gauge',
        points: [{ at: '2026-11-18T00:00:00Z', value: 2 }],
      },
    ])
    const wrapper = mount(HostMetricsPanel, {
      props: { cluster: 'c1', member: 'node-a' },
      global: { plugins: [ElementPlus], stubs: { Sparkline: true, ElProgress: true } },
    })
    await flushPromises()
    await nextTick()

    expect((wrapper.find('select').element as HTMLSelectElement).value).toBe('external')
    expect(api.metricQueryRange).toHaveBeenCalled()
    expect(wrapper.text()).toContain('2.00')
  })

  it('explains disabled external metrics and can switch back to the center', async () => {
    vi.mocked(api.hostMetrics).mockResolvedValue([])
    vi.mocked(api.metricQueryRange).mockRejectedValueOnce(
      new Error('external metric queries are not enabled'),
    )
    const wrapper = mount(HostMetricsPanel, {
      props: { cluster: 'c1', member: 'node-a' },
      global: { plugins: [ElementPlus], stubs: { Sparkline: true, ElProgress: true } },
    })
    await nextTick()

    await wrapper.find('select').setValue('external')
    await nextTick()
    await nextTick()
    expect(wrapper.text()).toContain('External metrics are not configured')
    expect(wrapper.text()).toContain('Set the Prometheus/VM query endpoint')

    const center = wrapper.findAll('button').find((button) => button.text().includes('Use center'))
    await center?.trigger('click')
    await nextTick()
    await nextTick()
    expect((wrapper.find('select').element as HTMLSelectElement).value).toBe('local')
    expect(api.hostMetrics).toHaveBeenCalledTimes(2)
  })

  it('shows an empty state without telemetry', async () => {
    ;(api.hostMetrics as any).mockResolvedValue([])
    const wrapper = mount(HostMetricsPanel, {
      props: { cluster: 'c1', member: 'node-a' },
      global: {
        stubs: { Sparkline: true, ElButton: true, ElProgress: true, ElAlert: true },
      },
    })
    await nextTick()
    await nextTick()
    expect(wrapper.text()).toContain('No agent telemetry yet')
  })
})
