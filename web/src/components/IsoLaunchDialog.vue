<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'

import { useInstancesStore } from '@/stores/instances'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{
  modelValue: boolean
  cluster: string
  isoName: string
  isoPool: string
  networks: string[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  launched: [name: string]
}>()

const instances = useInstancesStore()
const { t } = useI18n()

const form = reactive({
  name: '',
  network: '',
  rootDisk: '',
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      form.name = ''
      form.network = ''
      form.rootDisk = ''
    }
  },
  { immediate: true },
)

async function submit(): Promise<void> {
  if (!form.name.trim()) {
    ElMessage.error(t('iso.nameRequired'))
    return
  }

  try {
    await instances.launchFromISO(props.cluster, props.isoPool, props.isoName, {
      name: form.name.trim(),
      network: form.network || undefined,
      root_disk: form.rootDisk.trim() || undefined,
    })
    ElMessage.success(t('iso.launched', { name: form.name.trim() }))
    emit('launched', form.name.trim())
    visible.value = false
  } catch (err) {
    ElMessage.error(t('iso.launchFailed', { error: errorMessage(err) }))
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`${t('iso.launchTitle')} · ${isoName}`"
    width="480px"
    destroy-on-close
  >
    <p class="launch-hint">{{ t('iso.launchHint') }}</p>

    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item :label="t('iso.vmName')">
        <el-input v-model="form.name" placeholder="installer-01" />
      </el-form-item>

      <el-form-item :label="t('form.network')">
        <el-select v-model="form.network" clearable style="width: 100%">
          <el-option
            v-for="network in networks"
            :key="network"
            :label="network"
            :value="network"
          />
        </el-select>
        <p class="field-hint">{{ t('iso.networkHint') }}</p>
      </el-form-item>

      <el-form-item :label="t('form.rootDisk')">
        <el-input v-model="form.rootDisk" placeholder="20GiB" />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="instances.saving" @click="submit">
        {{ t('iso.launch') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.launch-hint {
  margin-bottom: 12px;
  color: var(--fleet-ink-soft);
  font-size: 13px;
}

.field-hint {
  margin-top: 4px;
  color: var(--fleet-ink-soft);
  font-size: 12px;
  line-height: 1.45;
}
</style>
