<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type {
  ClusterSummary,
  CreateInstanceRequest,
  InstanceDetail,
  InstanceImage,
  UpdateInstanceRequest,
} from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'
import { formatBytes } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    mode: 'create' | 'edit'
    clusters: ClusterSummary[]
    cluster?: string
    instance?: InstanceDetail | null
  }>(),
  {
    cluster: '',
    instance: null,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: [detail: InstanceDetail]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const form = reactive({
  cluster: '',
  name: '',
  type: 'container' as 'container' | 'virtual-machine',
  image: '',
  description: '',
  profiles: 'default',
  cpu: '',
  memory: '',
  rootDiskSize: '',
  storagePool: '',
  network: '',
  ipv4: '',
  ipv6: '',
  start: true,
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const isEdit = computed(() => props.mode === 'edit')
const title = computed(() => (isEdit.value ? t('form.editTitle') : t('form.createTitle')))
const clusterLocked = computed(() => Boolean(props.cluster))
const onlineClusters = computed(() => props.clusters.filter((cluster) => cluster.online))

const images = computed(() => instances.images(form.cluster))
const filteredImages = computed(() =>
  images.value.filter((image) => !image.type || image.type === form.type),
)
const pools = computed(() => instances.resources(form.cluster)?.storage_pools ?? [])
const networks = computed(() => instances.resources(form.cluster)?.networks ?? [])

let pendingCluster = ''

const nicDevice = computed(() => {
  if (!props.instance) {
    return null
  }

  const entry = Object.entries(props.instance.devices).find(([, device]) => device.type === 'nic')
  return entry ? { name: entry[0], device: entry[1] } : null
})

function resetForm(): void {
  form.cluster = props.cluster || onlineClusters.value[0]?.name || ''
  form.name = ''
  form.type = 'container'
  form.image = ''
  form.description = ''
  form.profiles = 'default'
  form.cpu = ''
  form.memory = ''
  form.rootDiskSize = ''
  form.storagePool = ''
  form.network = ''
  form.ipv4 = ''
  form.ipv6 = ''
  form.start = true

  if (!isEdit.value || !props.instance) {
    return
  }

  const detail = props.instance
  form.cluster = detail.cluster
  form.name = detail.name
  form.type = detail.type === 'virtual-machine' ? 'virtual-machine' : 'container'
  form.description = detail.description ?? ''
  form.profiles = detail.profiles.join(', ')
  form.cpu = detail.config['limits.cpu'] ?? ''
  form.memory = detail.config['limits.memory'] ?? ''
  form.rootDiskSize = detail.devices.root?.size ?? ''
  form.storagePool = detail.storage_pool ?? ''

  if (nicDevice.value) {
    form.network = nicDevice.value.device.network ?? ''
    form.ipv4 = nicDevice.value.device['ipv4.address'] ?? ''
    form.ipv6 = nicDevice.value.device['ipv6.address'] ?? ''
  }
}

function imageLabel(image: InstanceImage): string {
  const name = image.aliases[0] || image.os || image.fingerprint.slice(0, 12)
  const release = image.release ? ` ${image.release}` : ''
  return `${name}${release} · ${image.type || 'container'} · ${formatBytes(image.size)}`
}

async function loadClusterResources(cluster: string): Promise<void> {
  if (!cluster || pendingCluster === cluster) {
    return
  }

  pendingCluster = cluster
  try {
    await Promise.all([instances.fetchImages(cluster), instances.fetchResources(cluster)])
  } finally {
    pendingCluster = ''
  }

  if (isEdit.value) {
    return
  }

  const poolNames = instances.resources(cluster)?.storage_pools.map((pool) => pool.name) ?? []
  if (!form.storagePool || !poolNames.includes(form.storagePool)) {
    form.storagePool = poolNames[0] ?? ''
  }

  const networkNames = instances.resources(cluster)?.networks.map((network) => network.name) ?? []
  if (!form.network || !networkNames.includes(form.network)) {
    form.network = networkNames[0] ?? ''
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    resetForm()
    if (form.cluster) {
      void loadClusterResources(form.cluster)
    }
  },
  { immediate: true },
)

watch(
  () => form.cluster,
  (cluster, previous) => {
    if (!cluster || cluster === previous) {
      return
    }

    void loadClusterResources(cluster)
  },
)

watch(
  () => form.type,
  () => {
    const selected = images.value.find((image) => image.fingerprint === form.image)
    if (selected && selected.type && selected.type !== form.type) {
      form.image = ''
    }
  },
)

function createRequest(): CreateInstanceRequest {
  const request: CreateInstanceRequest = {
    name: form.name.trim(),
    type: form.type,
    image: form.image,
    start: form.start,
  }

  if (form.description.trim()) {
    request.description = form.description.trim()
  }

  const profiles = form.profiles
    .split(',')
    .map((profile) => profile.trim())
    .filter(Boolean)
  if (profiles.length > 0) {
    request.profiles = profiles
  }
  if (form.cpu.trim()) {
    request.cpu = form.cpu.trim()
  }
  if (form.memory.trim()) {
    request.memory = form.memory.trim()
  }
  if (form.rootDiskSize.trim()) {
    request.root_disk_size = form.rootDiskSize.trim()
  }
  if (form.storagePool) {
    request.storage_pool = form.storagePool
  }
  if (form.network) {
    request.network = form.network
  }
  if (form.ipv4.trim()) {
    request.ipv4 = form.ipv4.trim()
  }
  if (form.ipv6.trim()) {
    request.ipv6 = form.ipv6.trim()
  }

  return request
}

function updateRequest(): UpdateInstanceRequest {
  const request: UpdateInstanceRequest = {
    description: form.description,
    cpu: form.cpu.trim(),
    memory: form.memory.trim(),
    root_disk_size: form.rootDiskSize.trim(),
  }

  if (nicDevice.value) {
    request.device = nicDevice.value.name
    request.ipv4 = form.ipv4.trim()
    request.ipv6 = form.ipv6.trim()
    if (form.network) {
      request.network = form.network
    }
  }

  return request
}

function validate(): string {
  if (!isEdit.value) {
    if (!form.cluster) {
      return t('form.selectClusterFirst')
    }
    if (!form.name.trim()) {
      return t('form.validationName')
    }
    if (!form.image) {
      return t('form.validationImage')
    }
  }

  return ''
}

async function submit(): Promise<void> {
  const validation = validate()
  if (validation) {
    ElMessage.error(validation)
    return
  }

  try {
    const detail = isEdit.value
      ? await instances.updateInstance(form.cluster, form.name, updateRequest())
      : await instances.createInstance(form.cluster, createRequest())

    ElMessage.success(
      isEdit.value
        ? t('form.updated', { name: detail.name })
        : t('form.created', { name: detail.name }),
    )
    emit('saved', detail)
    visible.value = false
  } catch (err) {
    ElMessage.error(t('form.failed', { error: errorMessage(err) }))
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="title" width="640px" destroy-on-close>
    <el-form label-position="top" class="instance-form" @submit.prevent="submit">
      <div class="form-grid">
        <el-form-item v-if="!clusterLocked && !isEdit" :label="t('form.cluster')">
          <el-select v-model="form.cluster" style="width: 100%">
            <el-option
              v-for="option in onlineClusters"
              :key="option.name"
              :label="option.display_name"
              :value="option.name"
            />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('form.name')">
          <el-input v-model="form.name" :disabled="isEdit" :placeholder="t('form.namePlaceholder')" />
          <p class="field-hint">{{ t('form.nameHint') }}</p>
        </el-form-item>

        <el-form-item v-if="!isEdit" :label="t('form.type')">
          <el-radio-group v-model="form.type">
            <el-radio-button value="container">{{ t('common.container') }}</el-radio-button>
            <el-radio-button value="virtual-machine">
              {{ t('common.virtualMachine') }}
            </el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item v-if="!isEdit" :label="t('form.image')" class="form-span">
          <el-select
            v-model="form.image"
            filterable
            :loading="instances.imagesLoading"
            :placeholder="t('form.imagePlaceholder')"
            style="width: 100%"
          >
            <el-option
              v-for="image in filteredImages"
              :key="image.fingerprint"
              :label="imageLabel(image)"
              :value="image.fingerprint"
            />
          </el-select>
          <p class="field-hint">
            {{ instances.imagesLoading ? t('form.loadingImages') : t('form.imageHint') }}
          </p>
        </el-form-item>

        <el-form-item :label="t('form.description')" class="form-span">
          <el-input
            v-model="form.description"
            :placeholder="t('form.descriptionPlaceholder')"
          />
        </el-form-item>

        <el-form-item v-if="!isEdit" :label="t('form.profiles')" class="form-span">
          <el-input v-model="form.profiles" :placeholder="t('form.profilesPlaceholder')" />
          <p class="field-hint">{{ t('form.profilesHint') }}</p>
        </el-form-item>

        <el-form-item :label="t('form.cpu')">
          <el-input v-model="form.cpu" :placeholder="t('form.cpuPlaceholder')" />
          <p class="field-hint">{{ t('form.cpuHint') }}</p>
        </el-form-item>

        <el-form-item :label="t('form.memory')">
          <el-input v-model="form.memory" :placeholder="t('form.memoryPlaceholder')" />
        </el-form-item>

        <el-form-item :label="t('form.rootDisk')">
          <el-input v-model="form.rootDiskSize" :placeholder="t('form.rootDiskPlaceholder')" />
          <p class="field-hint">{{ t('form.rootDiskHint') }}</p>
        </el-form-item>

        <el-form-item v-if="!isEdit" :label="t('form.storagePool')">
          <el-select v-model="form.storagePool" style="width: 100%">
            <el-option
              v-for="pool in pools"
              :key="pool.name"
              :label="`${pool.name} · ${pool.driver}`"
              :value="pool.name"
            />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('form.network')">
          <el-select
            v-model="form.network"
            :placeholder="t('form.networkPlaceholder')"
            clearable
            style="width: 100%"
          >
            <el-option
              v-for="network in networks"
              :key="network.name"
              :label="`${network.name} · ${network.type}`"
              :value="network.name"
            />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('form.ipv4')">
          <el-input v-model="form.ipv4" :placeholder="t('form.ipv4Placeholder')" />
        </el-form-item>

        <el-form-item :label="t('form.ipv6')">
          <el-input v-model="form.ipv6" :placeholder="t('form.ipv6Placeholder')" />
        </el-form-item>

        <el-form-item v-if="!isEdit" class="form-span">
          <el-checkbox v-model="form.start">{{ t('form.start') }}</el-checkbox>
        </el-form-item>
      </div>

      <p v-if="isEdit" class="field-hint">{{ t('form.editHint') }}</p>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="instances.saving" @click="submit">
        {{ isEdit ? t('form.save') : t('form.create') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.instance-form {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 18px;
}

.form-span {
  grid-column: 1 / -1;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}

@media (max-width: 720px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
