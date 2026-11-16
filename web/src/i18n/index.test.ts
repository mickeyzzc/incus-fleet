import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { beforeEach, describe, expect, it } from 'vitest'

import LocaleSwitch from '@/components/LocaleSwitch.vue'
import { SUPPORTED_LOCALES, currentLocale, detectLocale, i18n, setLocale } from './index'

describe('i18n', () => {
  beforeEach(() => {
    localStorage.clear()
    setLocale('en')
  })

  it('lists both supported languages', () => {
    expect(SUPPORTED_LOCALES.map((locale) => locale.value)).toEqual(['zh-CN', 'en'])
  })

  it('detects Chinese browsers', () => {
    const original = navigator.language
    localStorage.clear()
    Object.defineProperty(navigator, 'language', { value: 'zh-CN', configurable: true })
    expect(detectLocale()).toBe('zh-CN')
    Object.defineProperty(navigator, 'language', { value: original, configurable: true })
  })

  it('prefers a saved locale over the browser language', () => {
    localStorage.setItem('incus-fleet.locale', 'zh-CN')
    expect(detectLocale()).toBe('zh-CN')
  })

  it('ignores unsupported saved locales', () => {
    const original = navigator.language
    Object.defineProperty(navigator, 'language', { value: 'en-US', configurable: true })
    localStorage.setItem('incus-fleet.locale', 'fr')
    expect(detectLocale()).toBe('en')
    Object.defineProperty(navigator, 'language', { value: original, configurable: true })
  })

  it('switches locale, persists it, and updates the document language', () => {
    setLocale('zh-CN')

    expect(currentLocale()).toBe('zh-CN')
    expect(i18n.global.t('titles.dashboard')).toBe('总览')
    expect(localStorage.getItem('incus-fleet.locale')).toBe('zh-CN')
    expect(document.documentElement.lang).toBe('zh-CN')
  })

  it('changes locale through the selector', async () => {
    const wrapper = mount(LocaleSwitch, { global: { plugins: [ElementPlus] } })

    const select = wrapper.findComponent({ name: 'ElSelect' })
    expect(select.props('modelValue')).toBe('en')

    select.vm.$emit('update:modelValue', 'zh-CN')

    expect(currentLocale()).toBe('zh-CN')
    expect(localStorage.getItem('incus-fleet.locale')).toBe('zh-CN')
  })
})

describe('i18n degraded environments', () => {
  it('falls back to the browser language when storage is unavailable', () => {
    const original = Storage.prototype.getItem
    Storage.prototype.getItem = () => {
      throw new Error('storage blocked')
    }

    try {
      expect(detectLocale()).toBe('en')
    } finally {
      Storage.prototype.getItem = original
    }
  })

  it('keeps switching locales when persistence fails', () => {
    const original = Storage.prototype.setItem
    Storage.prototype.setItem = () => {
      throw new Error('storage blocked')
    }

    try {
      setLocale('zh-CN')
      expect(currentLocale()).toBe('zh-CN')
    } finally {
      Storage.prototype.setItem = original
      localStorage.removeItem('incus-fleet.locale')
    }
  })
})
