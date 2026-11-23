/**
 * Shared Incus size handling.
 *
 * Incus accepts byte suffixes (kB, MB, GB, TB, KiB, MiB, GiB, TiB, PiB) and,
 * for memory limits, percentages. Users frequently type a bare number; we treat
 * that as MiB so "512" does not silently become 512 bytes.
 */

const SIZE_PATTERN = /^(\d+(?:\.\d+)?)\s*(B|kB|KB|MB|GB|TB|PB|KiB|MiB|GiB|TiB|PiB|%)?$/i

export interface ParsedSize {
  valid: boolean
  /** Normalized value to send to the API (empty when input was empty). */
  value: string
  /** Why the value is invalid, for inline hints. */
  reason?: 'format' | 'zero'
}

export function parseSize(raw: string, options: { allowPercent?: boolean; allowEmpty?: boolean } = {}): ParsedSize {
  const input = raw.trim()
  if (!input) {
    return { valid: options.allowEmpty ?? true, value: '' }
  }

  const match = SIZE_PATTERN.exec(input)
  if (!match) {
    return { valid: false, value: input, reason: 'format' }
  }

  const amountText = match[1] ?? ''
  const unit = match[2] ?? 'MiB'
  const amount = Number.parseFloat(amountText)

  if (!Number.isFinite(amount) || amount <= 0) {
    return { valid: false, value: input, reason: 'zero' }
  }

  if (unit === '%' && !options.allowPercent) {
    return { valid: false, value: input, reason: 'format' }
  }

  return { valid: true, value: `${amountText}${unit === '%' ? '%' : unit}` }
}

/** Returns a user-facing error key suffix for an invalid size. */
export function sizeErrorKey(reason: ParsedSize['reason']): string {
  return reason === 'zero' ? 'validation.sizePositive' : 'validation.sizeFormat'
}

/** True when an Incus device name is syntactically valid. */
export function validDeviceName(name: string): boolean {
  return /^[A-Za-z0-9]([A-Za-z0-9_.-]*[A-Za-z0-9])?$/.test(name)
}

/** True when a guest path is an absolute Unix path. */
export function validGuestPath(path: string): boolean {
  return path.startsWith('/') && !path.includes('..')
}
