import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api'
import type { InstanceMetrics } from '@/api/types'
import InstanceMetricsPanel from './InstanceMetricsPanel.vue'

function sample(overrides: Partial<InstanceMetrics> = {}): InstanceMetrics {
  return {
    name: 'web',
    status: 'Running',
    pid: 42,
    processes: 7,
    cpu: { usage: 1_000_000_000, allocated_time: 0 },
    memory: { usage: 1024, usage_peak: 2048, total: 4096, swap_usage: 0, swap_usage_peak: 0 },
    disk: [{ mountpoint: '/', usage: 100, total: 400 }],
    network: [
      {
        interface: 'eth0',
        state: 'up',
        type: 'broadcast',
        rx_bytes: 1000, rx_packets: 10, rx_errors: 0, rx_dropped: 0,
        tx_bytes: 2000, tx_packets: 20, tx_errors: 0, tx_dropped: 0,
      },
    ],
    at: '2026-11-13T08:00:00Z',
    ...overrides,
  }
}

function mountPanel() {
  return mount(InstanceMetricsPanel, {
    props: { cluster: 'c1', instance: 'web' },
    global: { plugins: [ElementPlus] },
  })
}

describe('InstanceMetricsPanel', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    vi.mocked(api.instanceMetrics).mockResolvedValue(sample())
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('loads and renders the first sample', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(api.instanceMetrics).toHaveBeenCalledWith('c1', 'web')
    expect(wrapper.text()).toContain('Running')
    expect(wrapper.text()).toContain('eth0')
    // Memory share of the total is rendered as a percentage.
    expect(wrapper.text()).toContain('25')
  })

  it('derives CPU percent and network rates from consecutive samples', async () => {
    vi.mocked(api.instanceMetrics)
      .mockResolvedValueOnce(sample())
      .mockResolvedValueOnce(sample({
        cpu: { usage: 2_000_000_000, allocated_time: 0 },
        network: [
          sample().network[0] !== undefined
            ? {
                ...sample().network[0]!,
                rx_bytes: 3000,
                tx_bytes: 5000,
              }
            : sample().network[0]!,
        ],
        at: '2026-11-13T08:00:02Z',
      }))

    const wrapper = mountPanel()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    // 1s of CPU over a 2s window = 50% of one core.
    expect(wrapper.text()).toContain('50')
    // (3000-1000)/2 = 1000 B/s down, (5000-2000)/2 = 1500 B/s up (formatted).
    expect(wrapper.text()).toContain('1000 B/s')
    expect(wrapper.text()).toContain('1.5 KiB/s')
    expect(wrapper.vm).toBeTruthy()
  })

  it('keeps polling while mounted and stops on unmount', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(api.instanceMetrics).toHaveBeenCalledTimes(2)

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(9000)
    expect(api.instanceMetrics).toHaveBeenCalledTimes(2)
  })

  it('renders the error state when polling fails', async () => {
    vi.mocked(api.instanceMetrics).mockRejectedValue(new Error('agent down'))

    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain('agent down')
  })
})
