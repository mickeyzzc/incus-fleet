import type { FleetVolume } from '@/api/types'

/** Shared inventory cache so re-entering the storage page stays instant. */
export const fleetVolumeCache: {
  current: { items: FleetVolume[]; clusters: string[]; at: number } | null
} = {
  current: null,
}
