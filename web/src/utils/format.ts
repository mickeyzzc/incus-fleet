/** Human-readable formatting helpers used across the UI. */

const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'] as const

/** Formats a byte count with binary units. */
export function formatBytes(bytes: number, decimals = 1): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B'
  }

  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), UNITS.length - 1)
  const value = bytes / 1024 ** exponent
  const unit = UNITS[exponent] ?? 'B'

  if (exponent === 0) {
    return `${Math.round(value)} ${unit}`
  }

  return `${value.toFixed(decimals)} ${unit}`
}

/** Formats a percentage, clamped to 0-100. */
export function formatPercent(used: number, total: number): string {
  if (!Number.isFinite(total) || total <= 0) {
    return '0%'
  }

  const percent = Math.min(100, Math.max(0, (used / total) * 100))
  return `${percent.toFixed(1)}%`
}

/** Formats an RFC3339 timestamp for display, returning a dash for zero values. */
export function formatDateTime(value?: string | null): string {
  if (!value) {
    return '—'
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) {
    return '—'
  }

  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(date)
}

/** Formats the duration between two timestamps as a compact string. */
export function formatDuration(start?: string | null, end?: string | null): string {
  if (!start) {
    return '—'
  }

  const started = new Date(start)
  const finished = end ? new Date(end) : new Date()
  if (Number.isNaN(started.getTime()) || Number.isNaN(finished.getTime())) {
    return '—'
  }

  const seconds = Math.max(0, Math.round((finished.getTime() - started.getTime()) / 1000))
  if (seconds < 60) {
    return `${seconds}s`
  }

  const minutes = Math.floor(seconds / 60)
  const remainder = seconds % 60
  if (minutes < 60) {
    return `${minutes}m ${remainder}s`
  }

  const hours = Math.floor(minutes / 60)
  return `${hours}h ${minutes % 60}m`
}

/** Shortens a migration identifier for tables. */
export function shortID(id: string): string {
  if (id.length <= 10) {
    return id
  }

  return `${id.slice(0, 8)}…`
}

/** Returns the primary address for an instance, if any. */
export function primaryAddress(ipv4: string[], ipv6: string[]): string {
  return ipv4[0] ?? ipv6[0] ?? '—'
}
