<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'

import { api } from '@/api'

const props = withDefaults(defineProps<{
  modelValue: boolean
  cluster: string
  instance: string
  // instanceType selects the default session. Containers have no real serial
  // console in Incus (forkconsole fails), so they start in exec mode; VMs start
  // on the console.
  instanceType?: string
}>(), { instanceType: '' })

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const { t } = useI18n()

const mode = ref<'console' | 'exec'>('console')
const command = ref('')
const connected = ref(false)
const statusMessage = ref('')

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let socket: WebSocket | null = null
let execStarted = false
// Lines submitted while no session is live; replayed once the new shell opens.
let pendingLines: string[] = []


const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      void openSession()
      return
    }

    closeSession()
  },
  { immediate: true },
)

async function openSession(): Promise<void> {
  // Containers must use exec: attaching forkconsole to a container fails.
  mode.value = props.instanceType === 'container' ? 'exec' : 'console'
  statusMessage.value = ''
  // The dialog body mounts asynchronously; wait for the host element before
  // connecting so early output is not dropped.
  if (!(await mountTerminal())) {
    statusMessage.value = t('terminal.failed', { error: 'mount' })
    return
  }

  connect(mode.value)
}

async function mountTerminal(): Promise<boolean> {
  for (let attempt = 0; attempt < 30; attempt++) {
    // The retry timers can outlive the environment (dialog closed, or in
    // tests the document torn down after the test finished) — bail out
    // instead of raising an unhandled rejection from a timer.
    if (typeof document === 'undefined') {
      return false
    }

    const host = document.getElementById('terminal-host')
    if (host) {
      term = new Terminal({ convertEol: true, fontSize: 12 })
      fitAddon = new FitAddon()
      term.loadAddon(fitAddon)
      term.open(host)
      try {
        fitAddon.fit()
      } catch {
        // Layout is not measurable during transitions; ignore.
      }

      return true
    }

    await new Promise((resolve) => setTimeout(resolve, 100))
  }

  return false
}

function connect(kind: 'console' | 'exec'): void {
  closeSocket()

  execStarted = false
  socket = new WebSocket(api.terminalURL(props.cluster, props.instance, kind))
  socket.binaryType = 'arraybuffer'
  socket.onopen = () => {
    connected.value = true
    statusMessage.value = t('terminal.connected')
    for (const line of pendingLines.splice(0)) {
      sendLine(line)
    }
  }
  socket.onmessage = (event) => {
    term?.write(decodePayload(event.data))
  }
  socket.onclose = (event) => {
    connected.value = false
    // The backend reports session failures (agent missing, instance stopped,
    // console busy) through the close reason; hiding it left dead terminals
    // that only said "disconnected".
    if ((event?.code ?? 1000) !== 1000 && event?.reason) {
      statusMessage.value = t('terminal.failed', { error: event.reason })
      return
    }
    statusMessage.value = t('terminal.disconnected')
  }
  socket.onerror = () => {
    connected.value = false
    statusMessage.value = t('terminal.failed', { error: 'websocket' })
  }
}

function decodePayload(data: unknown): string {
  if (typeof data === 'string') {
    return data
  }

  return new TextDecoder().decode(data as ArrayBuffer)
}

/** Reopens the session after a disconnect, keeping the scrollback. */
function reconnect(): void {
  statusMessage.value = ''
  connect(mode.value)
}

function switchMode(next: 'console' | 'exec'): void {
  if (mode.value === next) {
    return
  }

  mode.value = next
  term?.clear()
  connect(next)
}

function submitCommand(): void {
  const line = command.value
  if (!line) {
    return
  }
  command.value = ''

  if (socket && socket.readyState === WebSocket.OPEN) {
    sendLine(line)
    return
  }

  // A connection is already opening for this mode: queue instead of replacing
  // it. Reconnecting here discarded the previous session's output, which made
  // the standard "switch to exec, type, press Enter" path appear broken.
  if (socket && socket.readyState === WebSocket.CONNECTING) {
    pendingLines.push(line)
    return
  }

  // The previous shell exited; open a fresh one and replay this line once it
  // is connected.
  pendingLines.push(line)
  connect('exec')
}

function sendLine(line: string): void {
  if (!socket || socket.readyState !== WebSocket.OPEN) {
    return
  }

  // Only exec sessions need a launch frame; console input goes straight to the
  // serial device. Keying this on mode as well as the flag prevents a console
  // session from consuming the flag and leaving the following exec session
  // without a launch request (which silently swallowed every command).
  if (mode.value === 'exec' && !execStarted) {
    execStarted = true
    socket.send(
      JSON.stringify({
        command: ['bash'],
        width: term?.cols ?? 80,
        height: term?.rows ?? 25,
      }),
    )
  }

  socket.send(new TextEncoder().encode(`${line}\n`))
}

function onResize(): void {
  if (!socket || socket.readyState !== WebSocket.OPEN) {
    return
  }

  socket.send(
    JSON.stringify({ type: 'resize', width: term?.cols ?? 80, height: term?.rows ?? 25 }),
  )
}

function closeSocket(): void {
  if (socket) {
    // Detach handlers first: late close/error events from a replaced session
    // must not flip the status or write into the new connection.
    socket.onopen = null
    socket.onmessage = null
    socket.onclose = null
    socket.onerror = null
    socket.close()
  }
  socket = null
  connected.value = false
}

function closeSession(): void {
  closeSocket()
  pendingLines = []
  term?.dispose()
  term = null
  fitAddon = null
  execStarted = false
}

onBeforeUnmount(closeSession)
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('terminal.title')} · ${instance}`"
    width="720px"
    destroy-on-close
    @closed="closeSession"
  >
    <div class="terminal-toolbar">
      <el-radio-group :model-value="mode" size="small" @update:model-value="switchMode($event as 'console' | 'exec')">
        <el-radio-button value="console">{{ t('terminal.console') }}</el-radio-button>
        <el-radio-button value="exec">{{ t('terminal.exec') }}</el-radio-button>
      </el-radio-group>
      <div class="terminal-status">
        <el-tag :type="connected ? 'success' : 'info'" size="small" effect="light">
          {{ statusMessage || (connected ? t('terminal.connected') : t('terminal.disconnected')) }}
        </el-tag>
        <el-button v-if="!connected" size="small" type="primary" plain @click="reconnect">
          {{ t('terminal.reconnect') }}
        </el-button>
      </div>
    </div>

    <div id="terminal-host" class="terminal-host" />

    <el-alert
      v-if="mode === 'console' && instanceType === 'virtual-machine'"
      :title="t('terminal.vmConsoleHint')"
      type="info"
      :closable="false"
      class="terminal-hint"
      show-icon
    />

    <div v-if="mode === 'exec'" class="exec-bar">
      <el-input
        v-model="command"
        :placeholder="t('terminal.commandPlaceholder')"
        @keyup.enter="submitCommand"
      />
      <el-button type="primary" :disabled="!command" @click="submitCommand">
        {{ t('common.start') }}
      </el-button>
      <el-button :disabled="!connected" @click="onResize">{{ t('common.refresh') }}</el-button>
    </div>
  </el-dialog>
</template>

<style scoped>
.terminal-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.terminal-status {
  display: flex;
  align-items: center;
  gap: 8px;
}

.terminal-host {
  height: 320px;
  background: #14171c;
  border-radius: 6px;
  padding: 6px;
}

.terminal-hint {
  margin-top: 10px;
}

.exec-bar {
  display: flex;
  gap: 10px;
  margin-top: 10px;
}
</style>
