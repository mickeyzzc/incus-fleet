import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type { InstanceAction, InstanceDetail, InstanceSummary } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

type InstanceRef = InstanceSummary | InstanceDetail

/**
 * Shared instance lifecycle actions: stop/restart confirmations, force retry
 * after action timeouts, delete confirmation, and per-row busy tracking.
 */
export function useInstanceActions(refresh: () => Promise<void>) {
  const instances = useInstancesStore()
  const { t } = useI18n()

  const actionKey = ref('')

  function keyOf(instance: InstanceRef): string {
    return `${instance.cluster}/${instance.name}`
  }

  function isBusy(instance: InstanceRef): boolean {
    return actionKey.value === keyOf(instance)
  }

  async function confirmAction(action: InstanceAction, name: string): Promise<boolean> {
    if (action !== 'stop' && action !== 'restart') {
      return true
    }

    try {
      await ElMessageBox.confirm(
        t(action === 'stop' ? 'instances.stopConfirm' : 'instances.restartConfirm', { name }),
        t(`instances.actionDone.${action}`),
        {
          type: 'warning',
          confirmButtonText: t(`common.${action}`),
          cancelButtonText: t('common.cancel'),
        },
      )
    } catch {
      return false
    }

    return true
  }

  function isTimeout(err: unknown): boolean {
    return /timed out|timeout|deadline/i.test(errorMessage(err))
  }

  async function runAction(
    instance: InstanceRef,
    action: InstanceAction,
    force: boolean,
  ): Promise<void> {
    await instances.instanceAction(instance.cluster, instance.name, action, force)
    ElMessage.success(
      t('instances.actionSuccess', {
        action: t(`instances.actionDone.${action}`),
        name: instance.name,
      }),
    )
  }

  async function act(instance: InstanceRef, action: InstanceAction): Promise<void> {
    if (!(await confirmAction(action, instance.name))) {
      return
    }

    actionKey.value = keyOf(instance)
    try {
      try {
        await runAction(instance, action, false)
      } catch (err) {
        if ((action === 'stop' || action === 'restart') && isTimeout(err)) {
          try {
            await ElMessageBox.confirm(
              t('instances.forceRetryConfirm'),
              t('instances.actionFailed', { error: errorMessage(err) }),
              {
                type: 'warning',
                confirmButtonText: t('common.yes'),
                cancelButtonText: t('common.no'),
              },
            )
          } catch {
            throw err
          }

          await runAction(instance, action, true)
        } else {
          throw err
        }
      }

      await refresh()
    } catch (err) {
      ElMessage.error(t('instances.actionFailed', { error: errorMessage(err) }))
    } finally {
      actionKey.value = ''
    }
  }

  // Delete confirmation state (#13 F5): a dialog with an explicit "delete
  // storage volumes" option replaces the plain message box, so instance-owned
  // volumes can be removed in the same step.
  const deleteTarget = ref<InstanceRef | null>(null)
  const deleteVolumes = ref(false)
  const deleting = computed(() =>
    deleteTarget.value ? actionKey.value === keyOf(deleteTarget.value) : false,
  )

  function confirmDelete(instance: InstanceRef): void {
    deleteVolumes.value = false
    deleteTarget.value = instance
  }

  function cancelDelete(): void {
    deleteTarget.value = null
  }

  async function submitDelete(): Promise<void> {
    const instance = deleteTarget.value
    if (!instance) {
      return
    }

    actionKey.value = keyOf(instance)
    try {
      await instances.deleteInstance(instance.cluster, instance.name, true, deleteVolumes.value)
      ElMessage.success(t('instances.deleteSuccess', { name: instance.name }))
      deleteTarget.value = null
      await refresh()
    } catch (err) {
      ElMessage.error(t('instances.actionFailed', { error: errorMessage(err) }))
    } finally {
      actionKey.value = ''
    }
  }

  return { actionKey, isBusy, act, confirmDelete, deleteTarget, deleteVolumes, deleting, cancelDelete, submitDelete }
}

/**
 * Runs a loader on an interval while the page stays visible.
 *
 * Skips ticks in background tabs, skips ticks while the previous round is
 * still in flight (so slow responses cannot stack requests), and backs off
 * when the loader keeps rejecting (doubling per consecutive failure, capped
 * at 8x the base interval) so a down backend is not hammered.
 */
export function useVisiblePolling(loader: () => Promise<void>, intervalMs = 15000): void {
  let timer: ReturnType<typeof setInterval> | undefined
  let busy = false
  let failures = 0

  const tick = (): void => {
    if (typeof document !== 'undefined' && document.visibilityState !== 'visible') {
      return
    }
    if (busy) {
      return
    }
    busy = true
    void loader()
      .then(() => {
        failures = 0
      })
      .catch(() => {
        failures += 1
        if (timer !== undefined) {
          clearInterval(timer)
          timer = setInterval(tick, Math.min(intervalMs * 2 ** failures, intervalMs * 8))
        }
      })
      .finally(() => {
        busy = false
      })
  }

  onMounted(() => {
    timer = setInterval(tick, intervalMs)
  })

  onUnmounted(() => {
    if (timer !== undefined) {
      clearInterval(timer)
      timer = undefined
    }
  })
}
