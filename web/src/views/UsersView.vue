<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import type { User, UserRole } from '@/api/types'
import { api } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { errorMessage } from '@/utils/errors'
import { formatDateTime } from '@/utils/format'

const auth = useAuthStore()
const { t } = useI18n()

const users = ref<User[]>([])
const loading = ref(false)
const saving = ref(false)
const dialogOpen = ref(false)
const editing = ref<User | null>(null)

const form = reactive({
  username: '',
  password: '',
  role: 'viewer' as UserRole,
  disabled: false,
  mustChangePassword: false,
})

const title = computed(() =>
  editing.value ? t('users.editTitle') : t('users.createTitle'),
)

const roles: UserRole[] = ['admin', 'operator', 'viewer']

function roleLabel(role: UserRole): string {
  switch (role) {
    case 'admin':
      return t('users.roleAdmin')
    case 'operator':
      return t('users.roleOperator')
    default:
      return t('users.roleViewer')
  }
}

async function load(): Promise<void> {
  loading.value = true
  try {
    users.value = await api.listUsers()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    loading.value = false
  }
}

onMounted(load)

function openCreate(): void {
  editing.value = null
  form.username = ''
  form.password = ''
  form.role = 'viewer'
  form.disabled = false
  form.mustChangePassword = false
  dialogOpen.value = true
}

function openEdit(user: User): void {
  editing.value = user
  form.username = user.username
  form.password = ''
  form.role = user.role
  form.disabled = user.disabled
  form.mustChangePassword = user.must_change_password
  dialogOpen.value = true
}

async function submit(): Promise<void> {
  if (!form.username.trim()) {
    ElMessage.error(t('users.username'))
    return
  }
  if (!editing.value && !form.password) {
    ElMessage.error(t('users.password'))
    return
  }

  saving.value = true
  try {
    if (editing.value) {
      const updated = await api.updateUser(editing.value.id, {
        username: form.username.trim(),
        password: form.password || undefined,
        role: form.role,
        disabled: form.disabled,
        must_change_password: form.mustChangePassword,
      })
      ElMessage.success(t('users.updated', { name: updated.username }))
    } else {
      const created = await api.createUser({
        username: form.username.trim(),
        password: form.password,
        role: form.role,
        disabled: form.disabled,
        must_change_password: form.mustChangePassword,
      })
      ElMessage.success(t('users.created', { name: created.username }))
    }
    dialogOpen.value = false
    await load()
  } catch (err) {
    ElMessage.error(t('users.saveFailed', { error: errorMessage(err) }))
  } finally {
    saving.value = false
  }
}

async function remove(user: User): Promise<void> {
  try {
    await ElMessageBox.confirm(
      t('users.deleteConfirm', { name: user.username }),
      t('common.delete'),
      { type: 'warning', confirmButtonText: t('common.delete'), cancelButtonText: t('common.cancel') },
    )
  } catch {
    return
  }

  try {
    await api.deleteUser(user.id)
    ElMessage.success(t('users.deleted', { name: user.username }))
    await load()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}
</script>

<template>
  <div class="page">
    <div class="page-header">
      <div>
        <h2>{{ t('users.title') }}</h2>
        <p class="subtitle">{{ t('users.subtitle') }}</p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="loading" @click="load">
          {{ t('common.refresh') }}
        </el-button>
        <el-button type="primary" :icon="Plus" @click="openCreate">
          {{ t('users.create') }}
        </el-button>
      </div>
    </div>

    <el-alert
      class="fleet-alert"
      type="info"
      :closable="false"
      show-icon
      :title="t('users.roleHint')"
    />

    <section class="panel">
      <div class="panel-body">
        <el-table v-loading="loading" :data="users" stripe row-key="id">
          <el-table-column :label="t('users.username')" min-width="180">
            <template #default="{ row }">
              <strong>{{ row.username }}</strong>
              <el-tag v-if="row.username === auth.username" size="small" effect="plain" class="self-tag">
                {{ t('users.self') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('users.role')" width="140">
            <template #default="{ row }">
              <el-tag size="small" effect="plain" :type="row.role === 'admin' ? 'danger' : row.role === 'operator' ? 'warning' : 'info'">
                {{ roleLabel(row.role) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('users.status')" width="120">
            <template #default="{ row }">
              <el-tag size="small" :type="row.disabled ? 'info' : 'success'" effect="light">
                {{ row.disabled ? t('users.statusDisabled') : t('users.statusActive') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.created')" width="190">
            <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="170" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openEdit(row)">
                {{ t('common.edit') }}
              </el-button>
              <el-button link type="danger" @click="remove(row)">
                {{ t('common.delete') }}
              </el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('users.empty')" />
          </template>
        </el-table>
      </div>
    </section>

    <el-dialog v-model="dialogOpen" :title="title" width="520px" destroy-on-close>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('users.username')">
          <el-input v-model="form.username" autocomplete="off" />
        </el-form-item>

        <el-form-item :label="t('users.password')">
          <el-input v-model="form.password" type="password" show-password autocomplete="new-password" />
          <p class="field-hint">
            {{ editing ? t('users.passwordKeepHint') : t('users.passwordHint') }}
          </p>
        </el-form-item>

        <el-form-item :label="t('users.role')">
          <el-select v-model="form.role" style="width: 100%">
            <el-option v-for="role in roles" :key="role" :label="roleLabel(role)" :value="role" />
          </el-select>
        </el-form-item>

        <el-form-item>
          <el-switch v-model="form.disabled" :active-text="t('users.disabled')" />
        </el-form-item>
        <el-form-item>
          <el-switch v-model="form.mustChangePassword" :active-text="t('users.mustChangePassword')" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submit">
          {{ editing ? t('common.save') : t('common.create') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.self-tag {
  margin-left: 8px;
}
</style>
