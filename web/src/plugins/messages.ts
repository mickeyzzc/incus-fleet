import type { PluginEntry, PluginMessages } from '@/api/types'
import i18n from '@/i18n'

/** Plugin-provided strings live under their own namespace, never in core keys. */
export const PLUGIN_MESSAGE_ROOT = 'plugins.text'

const SAFE_ID = /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/

/**
 * Merges a plugin's en/zh-CN tables. A missing Chinese string automatically
 * falls back to English through the shared i18n fallback locale.
 */
export function mergePluginMessages(id: string, messages?: PluginMessages): void {
  if (!SAFE_ID.test(id) || !messages) {
    return
  }

  for (const locale of ['en', 'zh-CN'] as const) {
    const table = messages[locale]
    if (table && Object.keys(table).length > 0) {
      i18n.global.mergeLocaleMessage(locale, {
        plugins: { text: { [id]: table } },
      })
    }
  }
}

/**
 * Renders a manifest effect sentence. A key in the shared vocabulary is
 * translated; anything else is already human text supplied by the manifest.
 */
export function humanText(value: string): string {
  const key = `plugins.effect.${value}`
  return i18n.global.te(key) ? i18n.global.t(key) : value
}

/** Collects the message tables declared by a set of installed plugins. */
export function pluginMessagesFor(entries: PluginEntry[]): Record<string, PluginMessages> {
  const tables: Record<string, PluginMessages> = {}
  for (const entry of entries) {
    if (entry.ui.messages && Object.keys(entry.ui.messages).length > 0) {
      tables[entry.id] = entry.ui.messages
    }
  }
  return tables
}
