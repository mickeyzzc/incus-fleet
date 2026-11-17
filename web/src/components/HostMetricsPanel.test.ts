import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
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
