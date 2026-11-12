import { createI18n } from 'vue-i18n'

import en from './locales/en'
import zhCN from './locales/zh-CN'

export type Locale = 'zh-CN' | 'en'

export const SUPPORTED_LOCALES: ReadonlyArray<{ value: Locale; label: string }> = [
  { value: 'zh-CN', label: '中文' },
  { value: 'en', label: 'English' },
]

const STORAGE_KEY = 'incus-fleet.locale'

function isLocale(value: string | null): value is Locale {
  return value === 'zh-CN' || value === 'en'
}

/** Reads a saved locale, otherwise follows the browser language. */
export function detectLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (isLocale(saved)) {
      return saved
    }
  } catch {
    // Storage can be unavailable in hardened browser contexts.
  }

  const language = typeof navigator === 'undefined' ? '' : navigator.language
  return language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}

export const i18n = createI18n({
  legacy: false,
  locale: detectLocale(),
  fallbackLocale: 'en',
  messages: {
    en,
    'zh-CN': zhCN,
  },
})

/** Switches the active locale and persists the choice. */
export function setLocale(locale: Locale): void {
  i18n.global.locale.value = locale

  try {
    localStorage.setItem(STORAGE_KEY, locale)
  } catch {
    // Persisting the preference is best-effort.
  }

  if (typeof document !== 'undefined') {
    document.documentElement.lang = locale
  }
}

/** Returns the active locale. */
export function currentLocale(): Locale {
  return i18n.global.locale.value as Locale
}

if (typeof document !== 'undefined') {
  document.documentElement.lang = currentLocale()
}

export default i18n
