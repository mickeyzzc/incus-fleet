/** Extracts a display message from an unknown thrown value. */
export function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message
  }

  if (typeof error === 'string' && error) {
    return error
  }

  return 'Unexpected error'
}
