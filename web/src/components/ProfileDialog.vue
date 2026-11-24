<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { Delete } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import KeyValueEditor from '@/components/KeyValueEditor.vue'
import SizeInput from '@/components/SizeInput.vue'
import type { Profile } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    cluster: string
    profile?: Profile | null
  }>(),
  {
    profile: null,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: [name: string]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const form = reactive({
  name: '',
  description: '',
  config: {} as Record<string, string>,
  devices: [] as Array<{ name: string; type: string; config: Record<string, string> }>,
})

const structured = reactive({
  cpu: '',
  memory: '',
  disk: '',
  autostart: '' as '' | 'true' | 'false',
})

const DEVICE_TYPES = ['disk', 'nic', 'proxy', 'gpu', 'usb', 'unix-char', 'unix-block', 'none']

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const isEdit = computed(() => Boolean(props.profile))
const title = computed(() => (isEdit.value ? t('admin.editProfile') : t('admin.addProfile')))

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    form.name = props.profile?.name ?? ''
    form.description = props.profile?.description ?? ''
    const config = { ...(props.profile?.config ?? {}) }
    structured.cpu = config['limits.cpu'] ?? ''
    structured.memory = config['limits.memory'] ?? ''
    structured.disk = config['limits.disk'] ?? ''
    const autostart = config['boot.autostart']
    structured.autostart = autostart === 'true' || autostart === 'false' ? autostart : ''
    delete config['limits.cpu']
    delete config['limits.memory']
    delete config['limits.disk']
    delete config['boot.autostart']
    form.config = config
    form.devices = Object.entries(props.profile?.devices ?? {}).map(([name, device]) => {
      const deviceConfig = { ...device }
      const type = deviceConfig.type ?? ''
      delete deviceConfig.type
      return { name, type, config: deviceConfig }
    })
  },
  { immediate: true },
)

function request(): { name?: string; description?: string; config?: Record<string, string>; devices?: Record<string, Record<string, string>> } {
  const config = { ...form.config }
  if (structured.cpu.trim()) config['limits.cpu'] = structured.cpu.trim()
  if (structured.memory) config['limits.memory'] = structured.memory
  if (structured.disk) config['limits.disk'] = structured.disk
  if (structured.autostart) config['boot.autostart'] = structured.autostart

  const payload = {
    description: form.description.trim(),
    config: Object.keys(config).length > 0 ? config : undefined,
  }

  const devices: Record<string, Record<string, string>> = {}
  for (const device of form.devices) {
    const name = device.name.trim()
    if (name) {
      devices[name] = {
        ...device.config,
        ...(device.type.trim() ? { type: device.type.trim() } : {}),
      }
    }
  }

  if (isEdit.value) {
    // Profile updates replace the device map wholesale.
    return { ...payload, devices }
  }

  return { ...payload, name: form.name.trim(), devices }
}

function validate(): string {
  if (!isEdit.value && !form.name.trim()) {
    return t('admin.validationName')
  }

  if (form.devices.some((device) => !device.name.trim())) {
    return t('admin.deviceNameRequired')
  }

  const names = form.devices.map((device) => device.name.trim()).filter(Boolean)
  if (new Set(names).size !== names.length) {
    return t('validation.duplicateDevice')
  }

  return ''
}

function addDevice(): void {
  form.devices.push({ name: '', type: '', config: {} })
}

function removeDevice(index: number): void {
  form.devices.splice(index, 1)
}

async function submit(): Promise<void> {
  const validation = validate()
  if (validation) {
    ElMessage.error(validation)
    return
  }

  const name = isEdit.value ? (props.profile?.name ?? '') : form.name.trim()
  try {
    if (isEdit.value) {
      await instances.updateProfile(props.cluster, name, request())
    } else {
      await instances.createProfile(props.cluster, request())
    }

    ElMessage.success(t('admin.profileSaved', { name }))
    emit('saved', name)
    visible.value = false
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="title" width="640px" destroy-on-close>
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('common.name')">
        <el-input v-model="form.name" :disabled="isEdit" />
      </el-form-item>

      <el-form-item :label="t('admin.description')">
        <el-input v-model="form.description" />
      </el-form-item>

      <el-form-item :label="t('admin.limitsCpu')">
        <el-input v-model="structured.cpu" placeholder="2" />
        <p class="field-hint">{{ t('admin.limitsCpuHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('admin.limitsMemory')">
        <SizeInput v-model="structured.memory" :units="['MiB', 'GiB']" default-unit="GiB" />
        <p class="field-hint">{{ t('admin.limitsMemoryHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('admin.limitsDisk')">
        <SizeInput v-model="structured.disk" :units="['MiB', 'GiB', 'TiB']" default-unit="GiB" />
        <p class="field-hint">{{ t('admin.limitsDiskHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('admin.autostart')">
        <el-radio-group v-model="structured.autostart">
          <el-radio-button label="">{{ t('admin.defaultOption') }}</el-radio-button>
          <el-radio-button label="true">{{ t('admin.enabled') }}</el-radio-button>
          <el-radio-button label="false">{{ t('admin.disabled') }}</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item :label="t('admin.config')">
        <KeyValueEditor
          v-model="form.config"
          :key-placeholder="t('admin.keyPlaceholder')"
          :value-placeholder="t('admin.valuePlaceholder')"
          :add-label="t('admin.addRow')"
        />
      </el-form-item>

      <el-form-item :label="t('admin.profileDevices')">
        <div class="device-list">
          <div v-for="(device, index) in form.devices" :key="index" class="device-block">
            <div class="device-head">
              <el-input
                v-model="device.name"
                :placeholder="t('admin.deviceName')"
                class="device-name"
              />
              <el-select
                v-model="device.type"
                class="device-type"
                filterable
                allow-create
                default-first-option
                :placeholder="t('admin.deviceType')"
              >
                <el-option v-for="type in DEVICE_TYPES" :key="type" :label="type" :value="type" />
              </el-select>
              <el-button
                :icon="Delete"
                circle
                plain
                type="danger"
                @click="removeDevice(index)"
              />
            </div>
            <KeyValueEditor
              v-model="device.config"
              :key-placeholder="t('admin.keyPlaceholder')"
              :value-placeholder="t('admin.valuePlaceholder')"
              :add-label="t('admin.addRow')"
            />
          </div>
          <el-button plain size="small" @click="addDevice">
            {{ t('admin.addDevice') }}
          </el-button>
        </div>
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="instances.saving" @click="submit">
        {{ isEdit ? t('common.save') : t('common.create') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.device-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.device-block {
  border: 1px solid var(--fleet-line, #dcdfe6);
  border-radius: 6px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.device-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.device-name,
.device-type {
  flex: 1;
}

.field-hint {
  margin: 4px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
