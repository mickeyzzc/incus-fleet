import type { Component } from 'vue'
import {
  Bell,
  Coin,
  Connection,
  DataLine,
  Grid,
  Monitor,
  Odometer,
  Setting,
  Tools,
  TrendCharts,
} from '@element-plus/icons-vue'

/**
 * Plugins declare an icon by name in their manifest. Fleet maps that name to a
 * small built-in set instead of executing plugin code in the shell.
 */
const ICONS: Record<string, Component> = {
  default: Grid,
  grid: Grid,
  dashboard: Odometer,
  monitor: Monitor,
  metrics: TrendCharts,
  logs: DataLine,
  notify: Bell,
  storage: Coin,
  automation: Tools,
  connection: Connection,
  settings: Setting,
}

export const PLUGIN_ICON_KEYS = Object.keys(ICONS)

export function pluginIcon(name?: string): Component {
  return (name && ICONS[name]) || Grid
}
