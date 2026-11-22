<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import type { ClusterMember, OverlayRequest, OverlayStatus } from '@/api/types'
import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    cluster: string
    members?: ClusterMember[]
    overlay?: OverlayStatus | null
  }>(),
  {
    members: () => [],
    overlay: null,
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
  subnet: '',
  vni: 0,
  gatewayMember: '',
  mtu: 1450,
  dhcpStart: '',
  dhcpEnd: '',
  underlayNIC: '',
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }

    const overlay = props.overlay
    form.name = overlay?.name ?? props.cluster
    form.subnet = overlay?.subnet ?? ''
    form.vni = overlay?.vni ?? 0
    form.gatewayMember = overlay?.gateway_member ?? props.members[0]?.name ?? ''
    form.mtu = overlay?.mtu ?? 1450
    form.dhcpStart = overlay?.dhcp_start ?? ''
    form.dhcpEnd = overlay?.dhcp_end ?? ''
    form.underlayNIC = ''
  },
  { immediate: true },
)

function request(): OverlayRequest {
  const payload: OverlayRequest = {
    name: form.name.trim(),
    subnet: form.subnet.trim(),
    gateway_member: form.gatewayMember,
    mtu: Number(form.mtu),
  }

  if (form.vni > 0) {
    payload.vni = Number(form.vni)
  }
  if (form.dhcpStart.trim()) {
    payload.dhcp_start = form.dhcpStart.trim()
  }
  if (form.dhcpEnd.trim()) {
    payload.dhcp_end = form.dhcpEnd.trim()
  }
  if (form.underlayNIC.trim()) {
    payload.underlay_nic = form.underlayNIC.trim()
  }

  return payload
}

function validate(): string {
  if (!form.name.trim()) {
    return t('overlay.validationName')
  }
  if (!form.subnet.trim()) {
    return t('overlay.validationSubnet')
  }
  if (form.vni < 1 || form.vni > 16777215) {
    return t('overlay.validationVNI')
  }
  if (!form.gatewayMember) {
    return t('overlay.validationGateway')
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
    await instances.enableOverlay(props.cluster, request())
    ElMessage.success(t('overlay.saved', { name: form.name.trim() }))
    emit('saved', form.name.trim())
    visible.value = false
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="t('overlay.createTitle')" width="560px" destroy-on-close>
    <el-alert class="overlay-alert" :title="t('overlay.hint')" type="info" :closable="false" show-icon />

    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('overlay.name')">
        <el-input v-model="form.name" :disabled="Boolean(overlay)" />
      </el-form-item>

      <el-form-item :label="t('overlay.subnet')">
        <el-input v-model="form.subnet" placeholder="10.10.0.0/24" />
        <p class="field-hint">{{ t('overlay.subnetHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('overlay.vni')">
        <el-input-number v-model="form.vni" :min="1" :max="16777215" controls-position="right" />
        <p class="field-hint">{{ t('overlay.vniHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('overlay.gatewayMember')">
        <el-select v-model="form.gatewayMember" style="width: 100%">
          <el-option v-for="member in members" :key="member.name" :label="member.name" :value="member.name" />
        </el-select>
        <p class="field-hint">{{ t('overlay.gatewayHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('overlay.mtu')">
        <el-input-number v-model="form.mtu" :min="576" :max="9000" controls-position="right" />
        <p class="field-hint">{{ t('overlay.mtuHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('overlay.dhcpRange')">
        <div class="range-row">
          <el-input v-model="form.dhcpStart" :placeholder="t('overlay.dhcpStart')" />
          <span class="range-sep">–</span>
          <el-input v-model="form.dhcpEnd" :placeholder="t('overlay.dhcpEnd')" />
        </div>
        <p class="field-hint">{{ t('overlay.dhcpHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('overlay.underlayNIC')">
        <el-input v-model="form.underlayNIC" :placeholder="t('overlay.underlayNICHint')" />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="instances.saving" @click="submit">
        {{ t('overlay.enable') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.overlay-alert {
  margin-bottom: 12px;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}

.range-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.range-sep {
  color: var(--fleet-ink-soft);
}
</style>
