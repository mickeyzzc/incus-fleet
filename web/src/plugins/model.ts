import type {
  PluginEntry,
  PluginInstallRequest,
  PluginInstallTaskStatus,
  PluginMessages,
  PluginTaskEvent,
} from '@/api/types'
import i18n from '@/i18n'

/** One status word and one colour across every plugin surface. */
export type PluginStatus = 'installing' | 'healthy' | 'degraded' | 'stopped' | 'failed'

export const PLUGIN_STATUSES: readonly PluginStatus[] = [
  'installing', 'healthy', 'degraded', 'stopped', 'failed',
]

export interface PluginStatusView {
  status: PluginStatus
  /** i18n key for the status word; the word itself is never hardcoded. */
  labelKey: string
  /** Short i18n-resolved sentence explaining the state right now. */
  detail: string
}

/** Maps a descriptor to the shared vocabulary. Server data wins when present. */
export function pluginStatus(entry: PluginEntry): PluginStatusView {
  const status: PluginStatus = entry.status
    ?? (!entry.enabled ? 'stopped' : !entry.upstream ? 'degraded' : 'healthy')
  const { t } = i18n.global

  let detail: string
  if (entry.statusDetail) {
    detail = entry.statusDetail
  } else if (status === 'healthy') {
    detail = t('plugins.statusDetail.healthy', { target: entry.upstream ?? '' })
  } else {
    const key = status === 'degraded' && !entry.upstream ? 'noUpstream' : status
    detail = t(`plugins.statusDetail.${key}`)
  }

  return { status, labelKey: `plugins.status.${status}`, detail }
}

/** CSS modifier for the status dot; kept in one place so colours stay in sync. */
export function pluginDot(status: PluginStatus): string {
  switch (status) {
    case 'installing': return 'is-info'
    case 'healthy': return 'is-success'
    case 'degraded': return 'is-warning'
    case 'failed': return 'is-danger'
    default: return 'is-muted'
  }
}

/** Everything the shell needs to register a plugin's route and title. */
export interface PluginNavEntry {
  id: string
  path: string
  titleKey: string
  title: string
  icon: string
  requiresRole: string
  messages?: PluginMessages
}

export type InstallStepState = 'pending' | 'running' | 'done' | 'failed'

export interface PluginInstallStep {
  key: string
  labelKey: string
  state: InstallStepState
}

/** View state of the task panel; queued server states are shown as running. */
export type PluginInstallTaskViewState = 'running' | 'succeeded' | 'failed' | 'canceled'

export interface PluginInstallTask {
  pluginId: string
  pluginName: string
  /** Set when the task came from /plugin-installs; empty for the legacy path. */
  taskId?: string
  state: PluginInstallTaskViewState
  currentStep: number
  steps: PluginInstallStep[]
  error: string
  token: string
  request: PluginInstallRequest
  /** Server checkpoint percentage; undefined until a checkpoint is reported. */
  progress?: number
  attempt?: number
  events?: PluginTaskEvent[]
  /** Transient polling failure; never changes the task state. */
  pollError?: string
}

/** The orchestration steps a long install moves through, in order. */
export const INSTALL_STEP_DEFS = [
  { key: 'validate', labelKey: 'plugins.task.step.validate' },
  { key: 'placement', labelKey: 'plugins.task.step.placement' },
  { key: 'deploy', labelKey: 'plugins.task.step.deploy' },
  { key: 'health', labelKey: 'plugins.task.step.health' },
  { key: 'wiring', labelKey: 'plugins.task.step.wiring' },
] as const

/** Backend install has ten checkpoints; the panel groups them into five steps. */
const BACKEND_STEP_COUNT = 10
const UI_STEP_THRESHOLDS = [1, 2, 7, 8, 10] as const

/** Folds the server's detailed state vocabulary into the four panel states. */
export function pluginInstallState(state: string): PluginInstallTaskViewState {
  if (state === 'succeeded') return 'succeeded'
  if (state === 'failed') return 'failed'
  if (state === 'canceled') return 'canceled'
  return 'running'
}

/** Reads the numeric checkpoint from the server's JSON progress string. */
export function pluginProgressStep(progress?: string): number | undefined {
  if (!progress) return undefined
  try {
    const parsed = JSON.parse(progress) as { step?: unknown }
    const step = parsed?.step
    if (typeof step === 'number' && Number.isInteger(step)
      && step >= 0 && step <= BACKEND_STEP_COUNT) {
      return step
    }
  } catch {
    // Older or non-JSON progress strings carry no step information.
  }
  return undefined
}

/** Applies one task descriptor to the panel model without losing local data. */
export function applyPluginTaskStatus(
  task: PluginInstallTask,
  status: PluginInstallTaskStatus,
): PluginInstallTask {
  const state = pluginInstallState(status.state)
  const completed = pluginProgressStep(status.progress)
  const baseSteps = task.steps.length > 0
    ? task.steps
    : INSTALL_STEP_DEFS.map((step) => ({ ...step, state: 'pending' as const }))

  const provisional = baseSteps.map((step, index) => ({
    ...step,
    state: completed !== undefined && completed >= UI_STEP_THRESHOLDS[index]!
      ? 'done' as const
      : step.state,
  }))
  const found = provisional.findIndex((step) => step.state !== 'done')
  const firstPending = found === -1 ? provisional.length - 1 : found
  const steps = provisional.map((step, index): PluginInstallStep => {
    if (completed !== undefined && completed >= UI_STEP_THRESHOLDS[index]!) {
      return { ...step, state: 'done' }
    }
    if (state === 'succeeded') {
      return { ...step, state: 'done' }
    }
    const current = step.state
    if (index === firstPending) {
      if (state === 'failed') return { ...step, state: 'failed' }
      if (state === 'canceled') return { ...step, state: 'pending' }
      if (completed !== undefined) return { ...step, state: 'running' }
      return { ...step, state: current }
    }
    return { ...step, state: current === 'running' ? 'pending' : current }
  })
  const currentStep = steps.findIndex((step) => step.state !== 'done')

  return {
    ...task,
    taskId: status.id || task.taskId,
    state,
    progress: completed !== undefined
      ? Math.round((completed / BACKEND_STEP_COUNT) * 100)
      : state === 'succeeded' ? 100 : undefined,
    attempt: status.attempt,
    error: state === 'failed' ? (status.error || task.error) : '',
    events: status.events ?? task.events ?? [],
    pollError: '',
    currentStep: currentStep === -1 ? steps.length : currentStep,
    steps,
  }
}
