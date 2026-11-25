import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import TrafficSparkline from './TrafficSparkline.vue'

describe('TrafficSparkline', () => {
  it('renders labels above throughput and packet charts', () => {
    const wrapper = mount(TrafficSparkline, {
      props: {
        rx: [512, 1024, 2048],
        tx: [256, 512, 1024],
        rxPackets: [4, 12, 25.4],
        txPackets: [2, 6, 8],
      },
    })

    expect(wrapper.text()).toContain('↓ 2.0 KiB/s')
    expect(wrapper.text()).toContain('↑ 1.0 KiB/s')
    expect(wrapper.text()).toContain('↓ 25/s')
    expect(wrapper.text()).toContain('↑ 8/s')
    expect(wrapper.findAll('svg')).toHaveLength(2)
    expect(wrapper.find('svg.throughput').attributes('aria-label')).toContain('2.0 KiB/s')
    const paths = wrapper.findAll('path.spark-line')
    expect(paths.length).toBe(4)
    expect(paths[0]?.attributes('d')).toContain('M')
  })

  it('renders a flat baseline without samples', () => {
    const wrapper = mount(TrafficSparkline, { props: { rx: [], tx: [] } })

    expect(wrapper.text()).toContain('↓ 0 B/s')
    expect(wrapper.findAll('svg')).toHaveLength(1)
    expect(wrapper.find('circle.spark-dot.rx').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Packets')
  })

  it('accepts a custom title and clamps negative values', () => {
    const wrapper = mount(TrafficSparkline, {
      props: { rx: [-100, 500], tx: [0, 0], title: 'Fleet traffic' },
    })

    expect(wrapper.text()).toContain('Fleet traffic')
    expect(wrapper.find('svg').attributes('aria-label')).toContain('Fleet traffic')
  })
})
