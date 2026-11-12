<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { Delete } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import KeyValueEditor from '@/components/KeyValueEditor.vue'
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
  devices: [] as Array<{ name: string; config: Record<string, string> }>,
})

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
    form.config = { ...(props.profile?.config ?? {}) }
    form.devices = Object.entries(props.profile?.devices ?? {}).map(([name, device]) => ({
      name,
      config: { ...device },
    }))
  },
  { immediate: true },
)

function request(): { name?: string; description?: string; config?: Record<string, string>; devices?: Record<string, Record<string, string>> } {
  const payload = {
    description: form.description.trim(),
    config: Object.keys(form.config).length > 0 ? { ...form.config } : undefined,
  }

  const devices: Record<string, Record<string, string>> = {}
  for (const device of form.devices) {
    const name = device.name.trim()
    if (name) {
      devices[name] = { ...device.config }
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

  return ''
}

function addDevice(): void {
  form.devices.push({ name: '', config: {} })
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

.device-name {
  flex: 1;
}
</style>
