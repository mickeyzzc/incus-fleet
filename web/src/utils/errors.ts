import { ApiError } from '@/api'
import { i18n } from '@/i18n'

/** Maps backend error codes to localized message keys. */
const CODE_KEYS: Record<string, string> = {
  invalid_request: 'errors.invalidRequest',
  not_found: 'errors.notFound',
  conflict: 'errors.conflict',
  cluster_unavailable: 'errors.clusterUnavailable',
  invalid_token: 'login.invalidToken',
}

/** Extracts a display message from an unknown thrown value. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const key = CODE_KEYS[error.code]
    if (key) {
      return i18n.global.t(key)
    }

    if (error.message) {
      return error.message
    }
  }

  if (error instanceof Error && error.message) {
    return error.message
  }

  if (typeof error === 'string' && error) {
    return error
  }

  return i18n.global.t('errors.unknown')
}
