import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import Sparkline from './Sparkline.vue'

describe('Sparkline', () => {
  it('renders a polyline scaled to the value range', () => {
    const wrapper = mount(Sparkline, {
      props: { values: [0, 50, 100], width: 200, height: 40 },
    })

    const polyline = wrapper.find('polyline')
    expect(polyline.exists()).toBe(true)
    const points = polyline.attributes('points') ?? ''
    // First point sits on the baseline, last on the top edge.
    expect(points.split(' ')[0]).toBe('0.00,40.00')
    expect(points.split(' ')[2]).toBe('200.00,0.00')
  })

  it('keeps a flat line when all values are equal', () => {
    const wrapper = mount(Sparkline, {
      props: { values: [7, 7, 7], width: 100, height: 40 },
    })

    const points = (wrapper.find('polyline').attributes('points') ?? '').split(' ')
    for (const point of points) {
      expect(point.split(',')[1]).toBe('40.00')
    }
  })

  it('renders nothing without values', () => {
    const wrapper = mount(Sparkline, { props: { values: [] } })

    expect(wrapper.find('polyline').exists()).toBe(false)
    expect(wrapper.find('.sparkline-empty').exists()).toBe(true)
  })

  it('shifts the window once values exceed the capacity', () => {
    const wrapper = mount(Sparkline, {
      props: { values: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12], capacity: 4, width: 100, height: 20 },
    })

    const points = (wrapper.find('polyline').attributes('points') ?? '').split(' ')
    expect(points).toHaveLength(4)
  })
})
