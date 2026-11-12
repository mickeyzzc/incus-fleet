import type {
  ClusterResources,
  ClusterSummary,
  InstanceDetail,
  InstanceImage,
  InstanceSummary,
  Migration,
  MigrationStatus,
  Profile,
} from '@/api/types'

export function clusterSummary(overrides: Partial<ClusterSummary> = {}): ClusterSummary {
  return {
    name: 'c1',
    display_name: 'Cluster 62',
    endpoint: 'https://192.0.2.10:8443',
    online: true,
    server_name: 'node-1',
    server_version: '7.0.2',
    clustered: true,
    architectures: ['x86_64'],
    members: [
      {
        name: 'node-1',
        url: 'https://192.0.2.10:8443',
        status: 'Online',
        message: 'Fully operational',
        database: true,
        roles: ['database-leader'],
        architecture: 'x86_64',
      },
    ],
    instances: { total: 3, containers: 2, virtual_machines: 1, running: 2 },
    storage: { pool: 'default', used: 250_000_000_000, total: 1_000_000_000_000 },
    networks: ['incusbr0', 'eth0'],
    ...overrides,
  }
}

export function instanceSummary(overrides: Partial<InstanceSummary> = {}): InstanceSummary {
  return {
    name: 'web',
    cluster: 'c1',
    type: 'container',
    status: 'Running',
    status_code: 103,
    location: 'node-1',
    project: 'default',
    architecture: 'x86_64',
    description: 'web frontend',
    profiles: ['default'],
    ipv4: ['10.0.0.0.5'],
    ipv6: [],
    memory: 134_217_728,
    processes: 12,
    created_at: '2026-11-10T08:00:00Z',
    started_at: '2026-11-11T09:00:00Z',
    storage_pool: 'default',
    migration_stateful: false,
    live_migratable: false,
    ...overrides,
  }
}

export function instanceDetail(overrides: Partial<InstanceDetail> = {}): InstanceDetail {
  return {
    ...instanceSummary(),
    os: 'Debian GNU/Linux',
    os_version: '13',
    kernel_version: '6.12.0',
    hostname: 'web',
    ephemeral: false,
    config: { 'image.os': 'Debian', 'migration.stateful': 'true' },
    devices: { root: { type: 'disk', pool: 'default', path: '/' } },
    ...overrides,
  }
}

export function migration(overrides: Partial<Migration> = {}): Migration {  return {
    id: 'abcdef0123456789abcdef0123456789',
    source_cluster: 'c1',
    target_cluster: 'c2',
    instance: 'web',
    instance_type: 'container',
    mode: 'move',
    live: false,
    status: 'done' as MigrationStatus,
    progress: 'complete',
    error: '',
    created_at: '2026-11-11T10:00:00Z',
    updated_at: '2026-11-11T10:01:05Z',
    ...overrides,
  }
}

export function instanceImage(overrides: Partial<InstanceImage> = {}): InstanceImage {
  return {
    fingerprint: '053059ea4e28',
    aliases: ['debian'],
    type: 'container',
    architecture: 'x86_64',
    os: 'Debian',
    release: 'trixie',
    description: 'Debian trixie',
    size: 272_237_676,
    cached: false,
    created_at: '2026-11-01T00:00:00Z',
    ...overrides,
  }
}

export function profileSummary(overrides: Partial<Profile> = {}): Profile {
  return {
    name: 'default',
    description: '',
    config: {},
    devices: {},
    used_by: [],
    ...overrides,
  }
}

export function clusterResources(overrides: Partial<ClusterResources> = {}): ClusterResources {
  return {
    storage_pools: [
      {
        name: 'default',
        driver: 'dir',
        source: '/srv/incus',
        status: 'Created',
        config: { source: '/srv/incus' },
        used_by: ['/1.0/instances/web'],
        locations: [],
        total: 1_000_000_000_000,
        used: 250_000_000_000,
      },
    ],
    networks: [
      {
        name: 'incusbr0',
        type: 'bridge',
        managed: true,
        ipv4: '10.0.0.0.1/24',
        ipv6: 'none',
        config: { 'ipv4.address': '10.0.0.0.1/24', 'ipv6.address': 'none' },
        used_by: ['/1.0/instances/web'],
        locations: [],
      },
      {
        name: 'lo',
        type: 'loopback',
        managed: false,
        used_by: [],
        locations: [],
      },
    ],
    ...overrides,
  }
}
