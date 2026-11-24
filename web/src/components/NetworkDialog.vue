<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import KeyValueEditor from '@/components/KeyValueEditor.vue'
import type { NetworkResource } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    cluster: string
    network?: NetworkResource | null
  }>(),
  {
    network: null,
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
  type: '',
  description: '',
  config: {} as Record<string, string>,
})

const structured = reactive({
  ipv4: '',
  ipv4nat: '' as '' | 'true' | 'false',
  ipv6: '',
  ipv6nat: '' as '' | 'true' | 'false',
  mtu: '',
  dnsDomain: '',
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const isEdit = computed(() => Boolean(props.network))
const title = computed(() => (isEdit.value ? t('admin.editNetwork') : t('admin.addNetwork')))
const types = ['bridge', 'ovn', 'macvlan', 'sriov', 'physical']

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    form.name = props.network?.name ?? ''
    form.type = props.network?.type ?? ''
    form.description = props.network?.description ?? ''
    const config = { ...(props.network?.config ?? {}) }
    structured.ipv4 = config['ipv4.address'] ?? ''
    structured.ipv6 = config['ipv6.address'] ?? ''
    structured.mtu = config['bridge.mtu'] ?? ''
    structured.dnsDomain = config['dns.domain'] ?? ''
    const ipv4nat = config['ipv4.nat']
    const ipv6nat = config['ipv6.nat']
    structured.ipv4nat = ipv4nat === 'true' || ipv4nat === 'false' ? ipv4nat : ''
    structured.ipv6nat = ipv6nat === 'true' || ipv6nat === 'false' ? ipv6nat : ''
    for (const key of ['ipv4.address', 'ipv4.nat', 'ipv6.address', 'ipv6.nat', 'bridge.mtu', 'dns.domain']) {
      delete config[key]
    }
    form.config = config
  },
  { immediate: true },
)

function request(): { name?: string; type?: string; description?: string; config?: Record<string, string> } {
  const config = { ...form.config }
  if (structured.ipv4.trim()) config['ipv4.address'] = structured.ipv4.trim()
  if (structured.ipv4nat) config['ipv4.nat'] = structured.ipv4nat
  if (structured.ipv6.trim()) config['ipv6.address'] = structured.ipv6.trim()
  if (structured.ipv6nat) config['ipv6.nat'] = structured.ipv6nat
  if (structured.mtu.trim()) config['bridge.mtu'] = structured.mtu.trim()
  if (structured.dnsDomain.trim()) config['dns.domain'] = structured.dnsDomain.trim()
  const payload = {
    description: form.description.trim(),
    config: Object.keys(config).length > 0 ? config : undefined,
  }

  if (isEdit.value) {
    return payload
  }

  return { ...payload, name: form.name.trim(), type: form.type }
}

function validate(): string {
  if (!isEdit.value) {
    if (!form.name.trim()) {
      return t('admin.validationName')
    }
    if (!form.type) {
      return t('admin.validationType')
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

  const name = isEdit.value ? (props.network?.name ?? '') : form.name.trim()
  try {
    if (isEdit.value) {
      await instances.updateNetwork(props.cluster, name, request())
    } else {
      await instances.createNetwork(props.cluster, request())
    }

    ElMessage.success(t('admin.networkSaved', { name }))
    emit('saved', name)
    visible.value = false
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="title" width="560px" destroy-on-close>
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('cluster.networkName')">
        <el-input v-model="form.name" :disabled="isEdit" />
      </el-form-item>

      <el-form-item :label="t('cluster.networkType')">
        <el-select
          v-model="form.type"
          :disabled="isEdit"
          filterable
          style="width: 100%"
        >
          <el-option v-for="type in types" :key="type" :label="type" :value="type" />
        </el-select>
      </el-form-item>

      <el-form-item :label="t('admin.description')">
        <el-input v-model="form.description" />
      </el-form-item>

      <el-form-item :label="t('admin.ipv4Address')">
        <el-input v-model="structured.ipv4" placeholder="10.0.0.1/24" />
        <p class="field-hint">{{ t('admin.ipv4AddressHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('admin.ipv4Nat')">
        <el-radio-group v-model="structured.ipv4nat">
          <el-radio-button label="">{{ t('admin.defaultOption') }}</el-radio-button>
          <el-radio-button label="true">{{ t('admin.enabled') }}</el-radio-button>
          <el-radio-button label="false">{{ t('admin.disabled') }}</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item :label="t('admin.ipv6Address')">
        <el-input v-model="structured.ipv6" placeholder="fd42::1/64" />
      </el-form-item>

      <el-form-item :label="t('admin.ipv6Nat')">
        <el-radio-group v-model="structured.ipv6nat">
          <el-radio-button label="">{{ t('admin.defaultOption') }}</el-radio-button>
          <el-radio-button label="true">{{ t('admin.enabled') }}</el-radio-button>
          <el-radio-button label="false">{{ t('admin.disabled') }}</el-radio-button>
        </el-radio-group>
      </el-form-item>

      <el-form-item :label="t('admin.mtu')">
        <el-input v-model="structured.mtu" placeholder="1500" />
        <p class="field-hint">{{ t('admin.mtuHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('admin.dnsDomain')">
        <el-input v-model="structured.dnsDomain" placeholder="fleet.internal" />
      </el-form-item>

      <el-form-item :label="t('admin.config')">
        <KeyValueEditor
          v-model="form.config"
          :key-placeholder="t('admin.keyPlaceholder')"
          :value-placeholder="t('admin.valuePlaceholder')"
          :add-label="t('admin.addRow')"
        />
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
.field-hint {
  margin: 4px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
