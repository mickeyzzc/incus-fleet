import { onMounted, onUnmounted, ref } from 'vue'
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

  async function confirmDelete(instance: InstanceRef): Promise<void> {
    try {
      await ElMessageBox.confirm(
        t('instances.deleteConfirm', { name: instance.name }),
        t('instances.deleteTitle'),
        {
          type: 'warning',
          confirmButtonText: t('common.delete'),
          cancelButtonText: t('common.cancel'),
        },
      )
    } catch {
      return
    }

    actionKey.value = keyOf(instance)
    try {
      await instances.deleteInstance(instance.cluster, instance.name, true)
      ElMessage.success(t('instances.deleteSuccess', { name: instance.name }))
      await refresh()
    } catch (err) {
      ElMessage.error(t('instances.actionFailed', { error: errorMessage(err) }))
    } finally {
      actionKey.value = ''
    }
  }

  return { actionKey, isBusy, act, confirmDelete }
}

/** Runs a loader on an interval while the page stays visible. */
export function useVisiblePolling(loader: () => Promise<void>, intervalMs = 15000): void {
  let timer: ReturnType<typeof setInterval> | undefined

  onMounted(() => {
    timer = setInterval(() => {
      if (typeof document === 'undefined' || document.visibilityState === 'visible') {
        void loader()
      }
    }, intervalMs)
  })

  onUnmounted(() => {
    if (timer !== undefined) {
      clearInterval(timer)
    }
  })
}
