<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Box,
  Connection,
  Odometer,
  Switch as SwitchIcon,
  SwitchButton,
} from '@element-plus/icons-vue'

import { useAuthStore } from '@/stores/auth'
import { useClustersStore } from '@/stores/clusters'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const clusters = useClustersStore()

const publicPage = computed(() => route.meta.public === true)
const pageTitle = computed(() =>
  typeof route.meta.title === 'string' ? route.meta.title : 'Incus Fleet',
)
const activeMenu = computed(() =>
  route.name === 'cluster' ? `/clusters/${String(route.params.cluster ?? '')}` : route.path,
)

watch(
  () => auth.authenticated,
  (authenticated) => {
    if (authenticated && clusters.items.length === 0) {
      void clusters.fetchAll()
    }
  },
  { immediate: true },
)

async function logout(): Promise<void> {
  await auth.logout()
  await router.push({ name: 'login' })
}
</script>

<template>
  <router-view v-if="publicPage" />

  <el-container v-else class="app-shell">
    <el-aside width="236px" class="sidebar">
      <router-link to="/" class="brand">
        <span class="brand-mark">IF</span>
        <span class="brand-text">
          <strong>Incus Fleet</strong>
          <small>multi-cluster control</small>
        </span>
      </router-link>

      <el-menu :default-active="activeMenu" router class="nav-menu">
        <el-menu-item index="/">
          <el-icon><Odometer /></el-icon>
          <span>Dashboard</span>
        </el-menu-item>

        <el-sub-menu index="/clusters">
          <template #title>
            <el-icon><Connection /></el-icon>
            <span>Clusters</span>
          </template>
          <el-menu-item
            v-for="cluster in clusters.items"
            :key="cluster.name"
            :index="`/clusters/${cluster.name}`"
          >
            <span class="health-dot" :class="{ online: cluster.online }" />
            <span>{{ cluster.display_name }}</span>
          </el-menu-item>
          <el-menu-item v-if="clusters.items.length === 0" index="/clusters-empty" disabled>
            <span>No clusters loaded</span>
          </el-menu-item>
        </el-sub-menu>

        <el-menu-item index="/instances">
          <el-icon><Box /></el-icon>
          <span>Instances</span>
        </el-menu-item>

        <el-menu-item index="/migrations">
          <el-icon><SwitchIcon /></el-icon>
          <span>Migrations</span>
        </el-menu-item>
      </el-menu>

      <div class="sidebar-footer">
        <span class="health-dot" :class="{ online: clusters.onlineCount > 0 }" />
        <span>{{ clusters.onlineCount }}/{{ clusters.items.length }} clusters online</span>
      </div>
    </el-aside>

    <el-container class="main-column">
      <el-header class="topbar">
        <div>
          <h1>{{ pageTitle }}</h1>
          <p>Manage independent Incus clusters from one place.</p>
        </div>
        <div class="topbar-actions">
          <el-button :icon="SwitchButton" plain @click="logout">Sign out</el-button>
        </div>
      </el-header>

      <el-main class="content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>
