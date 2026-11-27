import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { describe, expect, it } from 'vitest'

import type { PluginInstallTask } from '@/plugins/model'
import PluginTaskPanel from './PluginTaskPanel.vue'

function task(overrides: Partial<PluginInstallTask> = {}): PluginInstallTask {
  return {
    pluginId: 'vm',
    pluginName: 'VictoriaMetrics',
    state: 'running',
    currentStep: 1,
    steps: [
      { key: 'validate', labelKey: 'plugins.task.step.validate', state: 'done' },
      { key: 'placement', labelKey: 'plugins.task.step.placement', state: 'running' },
      { key: 'deploy', labelKey: 'plugins.task.step.deploy', state: 'pending' },
      { key: 'health', labelKey: 'plugins.task.step.health', state: 'pending' },
      { key: 'wiring', labelKey: 'plugins.task.step.wiring', state: 'pending' },
    ],
    error: '',
    token: '',
    taskId: 'task-1',
    request: { id: 'vm' },
    ...overrides,
  }
}

function render(value: PluginInstallTask) {
  return mount(PluginTaskPanel, {
    props: { task: value },
    global: { plugins: [ElementPlus] },
  })
}

describe('PluginTaskPanel', () => {
  it('shows per-step progress instead of a blocking dialog', () => {
    const wrapper = render(task())
    expect(wrapper.attributes('data-state')).toBe('running')
    expect(wrapper.findAll('.task-step')).toHaveLength(5)
    expect(wrapper.find('[data-step="running"]').text()).toContain('Place resources')
    expect(wrapper.text()).toContain('Installing')
  })

  it('states the failed step, the error and the next action', async () => {
    const failed = task({
      state: 'failed',
      error: 'image import failed',
      steps: task().steps.map((step, index) => ({
        ...step,
        state: index === 2 ? 'failed' as const : index < 2 ? 'done' as const : 'pending' as const,
      })),
    })
    const wrapper = render(failed)

    expect(wrapper.text()).toContain('image import failed')
    expect(wrapper.text()).toContain('Failed at')
    expect(wrapper.text()).toContain('Create and configure the instance')

    const retry = wrapper.findAll('button').find((button) => button.text().includes('Resume install'))
    expect(retry).toBeDefined()
    await retry!.trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('reports completion and can be dismissed', async () => {
    const done = task({
      state: 'succeeded',
      steps: task().steps.map((step) => ({ ...step, state: 'done' as const })),
    })
    const wrapper = render(done)

    expect(wrapper.text()).toContain('Install finished')
    const dismiss = wrapper.findAll('button').find((button) => button.text().includes('Dismiss'))
    await dismiss!.trigger('click')
    expect(wrapper.emitted('dismiss')).toHaveLength(1)
  })

  it('prefers the server checkpoint percentage and shows the task id', () => {
    const wrapper = render(task({ progress: 60, attempt: 2, pollError: 'poll down' }))
    expect(wrapper.text()).toContain('60%')
    expect(wrapper.text()).toContain('task-1')
    expect(wrapper.text()).toContain('Attempt 2')
    expect(wrapper.text()).toContain('poll down')
  })

  it('offers a cancel action while the task is running', async () => {
    const wrapper = render(task())
    const cancel = wrapper.findAll('button').find((button) => button.text().includes('Cancel install'))
    expect(cancel).toBeDefined()
    await cancel!.trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })

  it('lets an operator resume a canceled task', async () => {
    const wrapper = render(task({ state: 'canceled' }))
    expect(wrapper.attributes('data-state')).toBe('canceled')
    expect(wrapper.text()).toContain('Install canceled')

    const resume = wrapper.findAll('button').find((button) => button.text().includes('Resume install'))
    expect(resume).toBeDefined()
    await resume!.trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('keeps the cancel action hidden for terminal tasks', () => {
    const wrapper = render(task({ state: 'succeeded' }))
    expect(wrapper.findAll('button').some((button) => button.text().includes('Cancel install'))).toBe(false)
  })
})
