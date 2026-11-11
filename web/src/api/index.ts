import { ApiClient } from './client'

/** Shared API client used by all Pinia stores. */
export const api = new ApiClient()

export { ApiClient, ApiError } from './client'
export type * from './types'
