<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import { api } from '@/api'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  instance: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const { t } = useI18n()

const connecting = ref(false)
const connected = ref(false)
const status = ref('')
let spice: { stop: (msg?: unknown) => void } | null = null

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      status.value = ''
      connected.value = false
      connect()
    } else {
      disconnect()
    }
  },
  { immediate: true },
)

async function connect(): Promise<void> {
  connecting.value = true
  try {
    // The dialog body mounts asynchronously; wait for the host element so
    // the SPICE client can attach its canvas before opening the websocket.
    await waitForHost()

    // Use Incus's own VGA console channel: the daemon talks to QEMU locally and
    // relays the SPICE protocol over the Incus API websocket. Bridging a raw
    // TCP port would only work when the fleet daemon runs on the same member as
    // the VM, and the SPICE listener is now bound to loopback for safety.
    status.value = t('desktop.preparing')

    const { SpiceMainConn } = await import('spice-client')
    spice = new SpiceMainConn({
      uri: api.vgaURL(props.cluster, props.instance),
      screen_id: 'remote-desktop-host',
      onsuccess: () => {
        connected.value = true
        status.value = ''
      },
      onerror: (err: unknown) => {
        status.value = describeDesktopError(err)
        connected.value = false
      },
    })
  } catch (err) {
    status.value = describeDesktopError(err)
  } finally {
    connecting.value = false
  }
}

/**
 * Turns the SPICE client's protocol errors into something actionable.
 *
 * The bundled client speaks SPICE protocol 2.2 while current QEMU servers
 * negotiate a newer dialect, and neither the Incus QEMU build nor Debian's own
 * ships the VNC display backend. That is a platform limitation, so the message
 * points at the working alternative instead of echoing "protocol mismatch".
 */
function describeDesktopError(err: unknown): string {
  const raw = errorMessage(err)
  if (/protocol mismatch|unexpected close|connection refused|websocket/i.test(raw)) {
    return t('desktop.incompatible')
  }

  return raw
}

/** Polls for the dialog-mounted host element (up to ~4s). */
async function waitForHost(): Promise<void> {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    if (document.getElementById('remote-desktop-host')) {
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  throw new Error('desktop host element not ready')
}

function disconnect(): void {
  try {
    spice?.stop()
  } catch {
    // the session may already be gone
  }
  spice = null
  connected.value = false
}

function reportError(): void {
  ElMessage.error(t('desktop.failed', { error: status.value }))
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('desktop.title')} · ${instance}`"
    width="960px"
    destroy-on-close
    @close="disconnect"
  >
    <p v-if="connecting" class="desktop-status">{{ t('desktop.connecting') }}</p>
    <p v-else-if="status" class="desktop-status desktop-error" @click="reportError">
      {{ t('desktop.failed', { error: status }) }}
    </p>
    <p v-else-if="connected" class="desktop-status">{{ t('desktop.connected') }}</p>
    <p v-else class="desktop-status">{{ t('desktop.waiting') }}</p>

    <div id="remote-desktop-host" class="desktop-host" />
  </el-dialog>
</template>

<style scoped>
.desktop-status {
  margin-bottom: 10px;
  color: var(--fleet-ink-soft);
  font-size: 13px;
}

.desktop-error {
  color: var(--el-color-danger);
}

.desktop-host {
  height: 540px;
  border: 1px solid var(--fleet-line, #dcdfe6);
  border-radius: 8px;
  background: #000;
  overflow: hidden;
}
</style>
