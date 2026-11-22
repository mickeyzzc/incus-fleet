<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import { api } from '@/api'
import type { InstanceAction } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

// Screenshot polling interval. QEMU's screendump round trip is well under a
// second, so this keeps interactive latency acceptable without flooding the
// host with SSH sessions.
const POLL_INTERVAL_MS = 800
// While the guest is stopped no frame can be served; poll slowly and only to
// notice a power-on instead of hammering the screenshot endpoint.
const STOPPED_POLL_INTERVAL_MS = 5000
// Drag-motion throttle: one absolute move per tick while a button is held.
const MOVE_THROTTLE_MS = 150
// Keys that only modify other keys; sending them alone is wasted round trips.
const BARE_MODIFIERS = new Set(['Control', 'Shift', 'Alt', 'Meta', 'Hyper', 'Super'])

const props = defineProps<{
  modelValue: boolean
  cluster: string
  instance: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const { t } = useI18n()
const instances = useInstancesStore()

const preparing = ref(false)
const live = ref(false)
const status = ref('')
const frameInfo = ref('')
const powerBusy = ref(false)

const canvas = ref<HTMLCanvasElement | null>(null)
let pollTimer: ReturnType<typeof setTimeout> | null = null
let pollBusy = false
let generation = 0
let dragTimer: ReturnType<typeof setTimeout> | null = null

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

// The guest's power state drives the toolbar buttons; a stopped VM shows the
// offline notice instead of a confusing screenshot error.
const guest = computed(() => instances.detail(props.cluster, props.instance))
const running = computed(() => (guest.value?.status ?? '').toLowerCase() === 'running')

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      void openConsole()
      void instances.fetchDetail(props.cluster, props.instance)
      return
    }

    stopConsole()
  },
  { immediate: true },
)

async function openConsole(): Promise<void> {
  generation += 1
  const session = generation
  preparing.value = true
  live.value = false
  status.value = ''

  try {
    await api.prepareDesktop(props.cluster, props.instance)
  } catch (err) {
    if (session !== generation) {
      return
    }
    preparing.value = false
    status.value = errorMessage(err)
    reportError()
    return
  }

  if (session !== generation) {
    return
  }
  preparing.value = false
  await waitForHost(session)
  if (session !== generation) {
    return
  }

  focusConsole()
  schedulePoll(session, 0)
}

/** Starts, stops, or restarts the guest from the console toolbar. */
async function power(action: InstanceAction): Promise<void> {
  powerBusy.value = true
  try {
    await instances.instanceAction(props.cluster, props.instance, action)
    ElMessage.success(t('desktop.powerDone', { action: t(`common.${action}`) }))
  } catch (err) {
    ElMessage.error(t('desktop.powerFailed', { error: errorMessage(err) }))
  } finally {
    powerBusy.value = false
  }
}

/** Polls for the dialog-mounted host element (up to ~4s). */
async function waitForHost(session: number): Promise<void> {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    if (session !== generation) {
      return
    }
    if (canvas.value) {
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  status.value = 'desktop host element not ready'
}

function schedulePoll(session: number, delay: number): void {
  if (pollTimer) {
    clearTimeout(pollTimer)
  }
  pollTimer = setTimeout(() => void pollFrame(session), delay)
}

async function pollFrame(session: number): Promise<void> {
  if (session !== generation || pollBusy) {
    return
  }

  pollBusy = true
  try {
    const blob = await api.desktopScreenshot(props.cluster, props.instance)
    if (session === generation) {
      await drawFrame(blob)
      live.value = true
      status.value = ''
    }
  } catch (err) {
    // Transient errors (guest restarting, member busy) keep the loop alive.
    if (session === generation) {
      // A stopped guest cannot serve frames; the offline notice explains the
      // situation, so the raw host error would only add noise.
      status.value = running.value ? errorMessage(err) : ''
    }
  } finally {
    pollBusy = false
  }

  if (session !== generation) {
    return
  }

  if (!running.value) {
    // Skip the screenshot fetch entirely and re-check the power state, so a
    // start from the toolbar (or elsewhere) is noticed without error spam.
    void instances.fetchDetail(props.cluster, props.instance).catch(() => {})
    schedulePoll(session, STOPPED_POLL_INTERVAL_MS)
    return
  }

  schedulePoll(session, POLL_INTERVAL_MS)
}

async function drawFrame(blob: Blob): Promise<void> {
  const host = canvas.value
  if (!host) {
    return
  }

  const context = host.getContext('2d')
  if (!context || typeof createImageBitmap !== 'function') {
    // jsdom and very old browsers have no decoding path; the status bar still
    // reflects a fresh frame even when nothing can be painted.
    frameInfo.value = describeFrame(host.width, host.height)
    return
  }

  const bitmap = await createImageBitmap(blob)
  try {
    host.width = bitmap.width
    host.height = bitmap.height
    context.drawImage(bitmap, 0, 0)
    frameInfo.value = describeFrame(bitmap.width, bitmap.height)
  } finally {
    bitmap.close()
  }
}

function describeFrame(width: number, height: number): string {
  return `${width}×${height}`
}

async function refreshFrame(): Promise<void> {
  await pollFrame(generation)
}

/** Maps one keyboard event onto the API's key event, or null to skip it. */
function keyEventOf(event: KeyboardEvent): { key: string; modifiers: string[] } | null {
  if (BARE_MODIFIERS.has(event.key)) {
    return null
  }

  const modifiers: string[] = []
  if (event.ctrlKey) modifiers.push('Control')
  if (event.altKey) modifiers.push('Alt')
  if (event.shiftKey) modifiers.push('Shift')
  if (event.metaKey) modifiers.push('Meta')

  return { key: event.key, modifiers }
}

async function onKeydown(event: KeyboardEvent): Promise<void> {
  const mapped = keyEventOf(event)
  if (!mapped) {
    // Bare modifier presses stay in the browser; they carry no guest action.
    return
  }

  // The console owns the keyboard while focused: stop the browser from
  // navigating (Tab), searching (F3) or triggering reserved shortcuts.
  event.preventDefault()

  try {
    await api.sendDesktopInput(props.cluster, props.instance, {
      type: 'key',
      key: mapped.key,
      modifiers: mapped.modifiers,
    })
    status.value = ''
  } catch (err) {
    status.value = errorMessage(err)
  }
}

/** Turns a pointer event on the canvas into 0..1 coordinates. */
function pointerOf(event: MouseEvent): { x: number; y: number } {
  const host = canvas.value
  if (!host) {
    return { x: 0, y: 0 }
  }

  const rect = host.getBoundingClientRect()
  const x = rect.width > 0 ? (event.clientX - rect.left) / rect.width : 0
  const y = rect.height > 0 ? (event.clientY - rect.top) / rect.height : 0

  return {
    x: Math.min(1, Math.max(0, x)),
    y: Math.min(1, Math.max(0, y)),
  }
}

async function sendPointer(event: {
  x: number
  y: number
  button?: string
  down?: boolean
  move?: boolean
}): Promise<void> {
  try {
    await api.sendDesktopInput(props.cluster, props.instance, { type: 'pointer', ...event })
    status.value = ''
  } catch (err) {
    status.value = errorMessage(err)
  }
}

async function onMousedown(event: MouseEvent): Promise<void> {
  const point = pointerOf(event)
  const button = BUTTON_NAMES.get(event.button) ?? 'left'
  await sendPointer({ ...point, button, down: true })
}

async function onMouseup(event: MouseEvent): Promise<void> {
  const point = pointerOf(event)
  const button = BUTTON_NAMES.get(event.button) ?? 'left'
  await sendPointer({ ...point, button, down: false })
}

// DOM button indices map onto the API's button names.
const BUTTON_NAMES = new Map([
  [0, 'left'],
  [1, 'middle'],
  [2, 'right'],
])

async function onContextmenu(event: MouseEvent): Promise<void> {
  // Right-click belongs to the guest; the browser menu would sit over the
  // console and swallow the matching mouseup.
  event.preventDefault()
}

function onMousemove(event: MouseEvent): void {
  // Hovering floods the host with SSH sessions, so only dragging (button held)
  // sends moves, throttled to one per tick.
  if (event.buttons === 0 || dragTimer) {
    return
  }

  const point = pointerOf(event)
  dragTimer = setTimeout(() => {
    dragTimer = null
    void sendPointer({ ...point, move: true })
  }, MOVE_THROTTLE_MS)
}

async function onWheel(event: WheelEvent): Promise<void> {
  event.preventDefault()
  const button = event.deltaY > 0 ? 'wheel-down' : 'wheel-up'
  await sendPointer({ ...pointerOf(event), button, down: true })
}

function focusConsole(): void {
  canvas.value?.focus()
}

async function sendCtrlAltDel(): Promise<void> {
  try {
    await api.sendDesktopInput(props.cluster, props.instance, {
      type: 'key',
      key: 'Delete',
      modifiers: ['Control', 'Alt'],
    })
    status.value = ''
  } catch (err) {
    status.value = errorMessage(err)
  }
}

function reportError(): void {
  ElMessage.error(t('desktop.failed', { error: status.value }))
}

function stopConsole(): void {
  generation += 1
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
  if (dragTimer) {
    clearTimeout(dragTimer)
    dragTimer = null
  }
  pollBusy = false
  live.value = false
  frameInfo.value = ''
  status.value = ''
}

onBeforeUnmount(stopConsole)
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('desktop.title')} · ${instance}`"
    width="960px"
    destroy-on-close
    @close="stopConsole"
  >
    <div class="desktop-toolbar">
      <el-tag :type="live ? 'success' : 'info'" size="small" effect="light">
        {{ preparing ? t('desktop.preparing') : live ? t('desktop.connected') : t('desktop.waiting') }}
      </el-tag>
      <el-tag v-if="guest" size="small" effect="plain" :type="running ? 'success' : 'info'">
        {{ guest.status }}
      </el-tag>
      <span v-if="frameInfo" class="desktop-frame">{{ frameInfo }}</span>
      <span v-if="status" class="desktop-error">{{ status }}</span>
      <div class="desktop-actions">
        <el-button
          v-if="!running"
          size="small"
          type="primary"
          :loading="powerBusy"
          @click="power('start')"
        >
          {{ t('common.start') }}
        </el-button>
        <template v-else>
          <el-button size="small" :loading="powerBusy" @click="power('restart')">
            {{ t('common.restart') }}
          </el-button>
          <el-button size="small" type="danger" plain :loading="powerBusy" @click="power('stop')">
            {{ t('common.stop') }}
          </el-button>
        </template>
        <el-button size="small" @click="refreshFrame">{{ t('common.refresh') }}</el-button>
        <el-button size="small" @click="sendCtrlAltDel">{{ t('desktop.ctrlAltDel') }}</el-button>
      </div>
    </div>

    <el-alert
      v-if="guest && !running"
      :title="t('desktop.offline')"
      type="warning"
      :closable="false"
      class="desktop-offline"
      show-icon
    />

    <canvas
      id="remote-desktop-host"
      ref="canvas"
      class="desktop-host"
      tabindex="0"
      @keydown="onKeydown"
      @mousedown="onMousedown"
      @mouseup="onMouseup"
      @mousemove="onMousemove"
      @wheel="onWheel"
      @contextmenu="onContextmenu"
    />
    <p class="desktop-hint">{{ t('desktop.hint') }}</p>
  </el-dialog>
</template>

<style scoped>
.desktop-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.desktop-frame {
  color: var(--fleet-ink-soft);
  font-size: 12px;
}

.desktop-error {
  color: var(--el-color-danger);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.desktop-actions {
  margin-left: auto;
  display: flex;
  gap: 8px;
}

.desktop-offline {
  margin-bottom: 10px;
}

.desktop-host {
  display: block;
  width: 100%;
  border: 1px solid var(--fleet-line, #dcdfe6);
  border-radius: 8px;
  background: #000;
  outline: none;
}

.desktop-hint {
  margin: 8px 0 0;
  color: var(--fleet-ink-soft);
  font-size: 12px;
}
</style>
