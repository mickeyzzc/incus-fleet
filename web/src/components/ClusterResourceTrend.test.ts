import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import ClusterResourceTrend from './ClusterResourceTrend.vue'

function series(name: string, value: number) {
  return {
    name,
    labels: {},
    type: 'gauge',
    points: [{ at: '2026-11-18T00:00:00Z', value }],
  }
}

describe('ClusterResourceTrend', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.metricQueryRange).mockReset()
    vi.mocked(api.metricQueryRange).mockResolvedValue([])
  })

  function mountChart() {
    return mount(ClusterResourceTrend, {
      props: { cluster: 'c1' },
      global: { stubs: { Sparkline: true } },
    })
  }

  it('renders the latest memory and storage samples', async () => {
    vi.mocked(api.metricQueryRange).mockResolvedValue([
      series('fleet_cluster_memory_used_bytes', 512),
      series('fleet_cluster_memory_total_bytes', 1024),
      series('fleet_cluster_storage_used_bytes', 256),
      series('fleet_cluster_storage_total_bytes', 2048),
    ])

    const wrapper = mountChart()
    await flushPromises()

    expect(api.metricQueryRange).toHaveBeenCalledWith(
      expect.stringContaining('fleet_cluster_memory_used_bytes'),
      '1h',
      '30s',
    )
    expect(wrapper.find('.trend-panel').exists()).toBe(true)
    expect(wrapper.text()).toContain('512 B')
    expect(wrapper.text()).toContain('1.0 KiB')
  })

  it('shows the query error when the fallback source is disabled', async () => {
    vi.mocked(api.metricQueryRange).mockRejectedValue(new Error('query unavailable'))

    const wrapper = mountChart()
    await flushPromises()

    expect(wrapper.find('.trend-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('query unavailable')
  })

  it('stays empty when the query returns no data at all', async () => {
    vi.mocked(api.metricQueryRange).mockResolvedValue(undefined as never)

    const wrapper = mountChart()
    await flushPromises()

    expect(wrapper.find('.trend-panel').exists()).toBe(false)
    expect(wrapper.find('.trend-error').exists()).toBe(false)
  })
})
