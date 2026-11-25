<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'

import { formatBytes } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    rx: number[]
    tx: number[]
    rxPackets?: number[]
    txPackets?: number[]
    title?: string
    compact?: boolean
  }>(),
  {
    rxPackets: () => [],
    txPackets: () => [],
    title: '',
    compact: false,
  },
)

const { t } = useI18n()
const uid = useId().replace(/[^a-zA-Z0-9_-]/g, '')
const rxGradient = `traffic-rx-${uid}`
const txGradient = `traffic-tx-${uid}`

const CHART_WIDTH = 100
const CHART_HEIGHT = 34
const PACKET_HEIGHT = 18

const rxLast = computed(() => props.rx.at(-1) ?? 0)
const txLast = computed(() => props.tx.at(-1) ?? 0)
const rxPacketsLast = computed(() => props.rxPackets.at(-1) ?? 0)
const txPacketsLast = computed(() => props.txPackets.at(-1) ?? 0)

const showPackets = computed(() => props.rxPackets.length > 0 || props.txPackets.length > 0)
const throughputMax = computed(() => chartMax(props.rx, props.tx))
const packetsMax = computed(() => chartMax(props.rxPackets, props.txPackets))

const title = computed(() => props.title || t('dashboard.liveTraffic'))
const ariaLabel = computed(
  () =>
    `${title.value}: ${t('traffic.rx')} ${formatBytes(rxLast.value)}/s, ${t('traffic.tx')} ${formatBytes(txLast.value)}/s`,
)

function chartMax(...series: number[][]): number {
  let max = 0
  for (const values of series) {
    for (const value of values) {
      if (Number.isFinite(value) && value > max) {
        max = value
      }
    }
  }

  // Leave a little headroom so a steady rate does not sit on the chart edge.
  return Math.max(1, max * 1.15)
}

function points(values: number[], max: number, height: number): Array<{ x: number; y: number }> {
  const data = values.length > 0 ? values : [0]
  const pad = 2
  const usable = height - pad * 2
  if (data.length === 1) {
    const y = pad + (1 - Math.min(1, data[0]! / max)) * usable
    return [
      { x: 0, y },
      { x: CHART_WIDTH, y },
    ]
  }

  return data.map((value, index) => ({
    x: (index / (data.length - 1)) * CHART_WIDTH,
    y: pad + (1 - Math.min(1, Math.max(0, value) / max)) * usable,
  }))
}

function linePath(values: number[], max: number, height: number): string {
  return points(values, max, height)
    .map((point, index) => `${index === 0 ? 'M' : 'L'}${point.x.toFixed(2)},${point.y.toFixed(2)}`)
    .join(' ')
}

function areaPath(values: number[], max: number, height: number): string {
  const data = points(values, max, height)
  const top = data
    .map((point, index) => `${index === 0 ? 'M' : 'L'}${point.x.toFixed(2)},${point.y.toFixed(2)}`)
    .join(' ')
  const last = data[data.length - 1]!
  return `${top} L${last.x.toFixed(2)},${height} L0,${height} Z`
}

function endpoint(values: number[], max: number, height: number): { x: number; y: number } {
  const data = points(values, max, height)
  return data[data.length - 1]!
}

const rxEnd = computed(() => endpoint(props.rx, throughputMax.value, CHART_HEIGHT))
const txEnd = computed(() => endpoint(props.tx, throughputMax.value, CHART_HEIGHT))
const rxPacketsEnd = computed(() => endpoint(props.rxPackets, packetsMax.value, PACKET_HEIGHT))
const txPacketsEnd = computed(() => endpoint(props.txPackets, packetsMax.value, PACKET_HEIGHT))

function rate(value: number): string {
  return `${formatBytes(value)}/s`
}

function packets(value: number): string {
  return `${Math.round(value)}/s`
}
</script>

<template>
  <div class="traffic-spark" :class="{ compact }">
    <div class="traffic-block">
      <div class="traffic-meta">
        <span class="traffic-kicker">{{ title }}</span>
        <span class="traffic-reading down">↓ {{ rate(rxLast) }}</span>
        <span class="traffic-reading up">↑ {{ rate(txLast) }}</span>
      </div>
      <svg
        class="spark throughput"
        :viewBox="`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`"
        preserveAspectRatio="none"
        role="img"
        :aria-label="ariaLabel"
      >
        <defs>
          <linearGradient :id="rxGradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="#3b82f6" stop-opacity="0.28" />
            <stop offset="100%" stop-color="#3b82f6" stop-opacity="0" />
          </linearGradient>
          <linearGradient :id="txGradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="#8b5cf6" stop-opacity="0.24" />
            <stop offset="100%" stop-color="#8b5cf6" stop-opacity="0" />
          </linearGradient>
        </defs>
        <path class="spark-area rx" :d="areaPath(rx, throughputMax, CHART_HEIGHT)" :fill="`url(#${rxGradient})`" />
        <path class="spark-area tx" :d="areaPath(tx, throughputMax, CHART_HEIGHT)" :fill="`url(#${txGradient})`" />
        <path
          class="spark-line rx"
          :d="linePath(rx, throughputMax, CHART_HEIGHT)"
          vector-effect="non-scaling-stroke"
        />
        <path
          class="spark-line tx"
          :d="linePath(tx, throughputMax, CHART_HEIGHT)"
          vector-effect="non-scaling-stroke"
        />
        <circle class="spark-dot rx" :cx="rxEnd.x" :cy="rxEnd.y" r="1.8" />
        <circle class="spark-dot tx" :cx="txEnd.x" :cy="txEnd.y" r="1.8" />
      </svg>
    </div>

    <div v-if="showPackets" class="traffic-block packets">
      <div class="traffic-meta">
        <span class="traffic-kicker">{{ t('dashboard.packets') }}</span>
        <span class="traffic-reading down">↓ {{ packets(rxPacketsLast) }}</span>
        <span class="traffic-reading up">↑ {{ packets(txPacketsLast) }}</span>
      </div>
      <svg
        class="spark packets"
        :viewBox="`0 0 ${CHART_WIDTH} ${PACKET_HEIGHT}`"
        preserveAspectRatio="none"
        aria-hidden="true"
      >
        <path
          class="spark-area rx"
          :d="areaPath(rxPackets, packetsMax, PACKET_HEIGHT)"
          :fill="`url(#${rxGradient})`"
        />
        <path
          class="spark-area tx"
          :d="areaPath(txPackets, packetsMax, PACKET_HEIGHT)"
          :fill="`url(#${txGradient})`"
        />
        <path
          class="spark-line rx"
          :d="linePath(rxPackets, packetsMax, PACKET_HEIGHT)"
          vector-effect="non-scaling-stroke"
        />
        <path
          class="spark-line tx"
          :d="linePath(txPackets, packetsMax, PACKET_HEIGHT)"
          vector-effect="non-scaling-stroke"
        />
        <circle class="spark-dot rx" :cx="rxPacketsEnd.x" :cy="rxPacketsEnd.y" r="1.6" />
        <circle class="spark-dot tx" :cx="txPacketsEnd.x" :cy="txPacketsEnd.y" r="1.6" />
      </svg>
    </div>
  </div>
</template>

<style scoped>
.traffic-spark {
  display: flex;
  flex-direction: column;
  gap: 7px;
  min-width: 0;
}

.traffic-block {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.traffic-meta {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
  flex-wrap: wrap;
}

.traffic-kicker {
  color: var(--fleet-ink-soft);
  font-size: 10.5px;
  font-weight: 650;
  letter-spacing: 0.055em;
  text-transform: uppercase;
  white-space: nowrap;
}

.traffic-reading {
  font-size: 12px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  line-height: 1.2;
  white-space: nowrap;
}

.traffic-reading.down {
  color: #2563eb;
}

.traffic-reading.up {
  color: #7c3aed;
}

.spark {
  display: block;
  width: 100%;
  height: 34px;
  overflow: visible;
}

.spark.packets {
  height: 18px;
  opacity: 0.9;
}

.spark-line {
  fill: none;
  stroke-width: 1.6;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.spark-line.rx {
  stroke: #3b82f6;
}

.spark-line.tx {
  stroke: #8b5cf6;
}

.spark-dot.rx {
  fill: #3b82f6;
}

.spark-dot.tx {
  fill: #8b5cf6;
}

.compact .spark {
  height: 26px;
}

.compact .traffic-reading {
  font-size: 11.5px;
}

@media (prefers-color-scheme: dark) {
  .traffic-reading.down {
    color: #7fb0ff;
  }

  .traffic-reading.up {
    color: #c4a7ff;
  }
}
</style>
