<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'

import { api } from '@/api'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  instance: string
}>()

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

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      openSession()
      return
    }

    closeSession()
  },
  { immediate: true },
)

function openSession(): void {
  mode.value = 'console'
  statusMessage.value = ''
  mountTerminal()
  connect('console')
}

function mountTerminal(): void {
  if (term) {
    return
  }

  const host = document.getElementById('terminal-host')
  if (!host) {
    return
  }

  term = new Terminal({ convertEol: true, fontSize: 12 })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(host)
  try {
    fitAddon.fit()
  } catch {
    // Layout is not measurable during transitions; ignore.
  }
}

function connect(kind: 'console' | 'exec'): void {
  closeSocket()

  socket = new WebSocket(api.terminalURL(props.cluster, props.instance, kind))
  socket.binaryType = 'arraybuffer'
  socket.onopen = () => {
    connected.value = true
    statusMessage.value = t('terminal.connected')
  }
  socket.onmessage = (event) => {
    term?.write(decodePayload(event.data))
  }
  socket.onclose = () => {
    connected.value = false
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

function switchMode(next: 'console' | 'exec'): void {
  if (mode.value === next) {
    return
  }

  mode.value = next
  execStarted = false
  term?.clear()
  connect(next)
}

function submitCommand(): void {
  const line = command.value
  if (!line || !socket || socket.readyState !== WebSocket.OPEN) {
    return
  }

  if (!execStarted) {
    execStarted = true
    socket.send(
      JSON.stringify({
        command: ['bash', '-c', line],
        width: term?.cols ?? 80,
        height: term?.rows ?? 25,
      }),
    )
  } else {
    socket.send(new TextEncoder().encode(`${line}\n`))
  }

  command.value = ''
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
  socket?.close()
  socket = null
  connected.value = false
}

function closeSession(): void {
  closeSocket()
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
      <el-tag :type="connected ? 'success' : 'info'" size="small" effect="light">
        {{ statusMessage || (connected ? t('terminal.connected') : t('terminal.disconnected')) }}
      </el-tag>
    </div>

    <div id="terminal-host" class="terminal-host" />

    <div v-if="mode === 'exec'" class="exec-bar">
      <el-input
        v-model="command"
        :placeholder="t('terminal.commandPlaceholder')"
        :disabled="!connected"
        @keyup.enter="submitCommand"
      />
      <el-button type="primary" :disabled="!connected" @click="submitCommand">
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

.terminal-host {
  height: 320px;
  background: #14171c;
  border-radius: 6px;
  padding: 6px;
}

.exec-bar {
  display: flex;
  gap: 10px;
  margin-top: 10px;
}
</style>
