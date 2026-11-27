import type { PluginEntry, PluginInstallRequest, PluginMessages } from '@/api/types'
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

export interface PluginInstallTask {
  pluginId: string
  pluginName: string
  state: 'running' | 'succeeded' | 'failed'
  currentStep: number
  steps: PluginInstallStep[]
  error: string
  token: string
  request: PluginInstallRequest
}

/** The orchestration steps a long install moves through, in order. */
export const INSTALL_STEP_DEFS = [
  { key: 'validate', labelKey: 'plugins.task.step.validate' },
  { key: 'placement', labelKey: 'plugins.task.step.placement' },
  { key: 'deploy', labelKey: 'plugins.task.step.deploy' },
  { key: 'health', labelKey: 'plugins.task.step.health' },
  { key: 'wiring', labelKey: 'plugins.task.step.wiring' },
] as const
