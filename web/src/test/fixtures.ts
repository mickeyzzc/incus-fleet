import type {
  ClusterSummary,
  InstanceDetail,
  InstanceSummary,
  Migration,
  MigrationStatus,
} from '@/api/types'

export function clusterSummary(overrides: Partial<ClusterSummary> = {}): ClusterSummary {
  return {
    name: 'c1',
    display_name: 'Cluster 62',
    endpoint: 'https://192.0.2.10:8443',
    online: true,
    server_name: 'node-a',
    server_version: '7.0.2',
    clustered: true,
    architectures: ['x86_64'],
    members: [
      {
        name: 'node-a',
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
    location: 'node-a',
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

export function migration(overrides: Partial<Migration> = {}): Migration {
  return {
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
