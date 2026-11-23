<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  Box,
  Grid,
  Odometer,
  SwitchButton,
  Collection,
  UserFilled,
  Coin,
  Fold,
  Expand,
} from '@element-plus/icons-vue'
import en from 'element-plus/es/locale/lang/en'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

import LocaleSwitch from '@/components/LocaleSwitch.vue'
import { useAuthStore } from '@/stores/auth'
import { useClustersStore } from '@/stores/clusters'
import { usePluginsStore } from '@/stores/plugins'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const clusters = useClustersStore()
const plugins = usePluginsStore()
const { t, locale } = useI18n()

const publicPage = computed(() => route.meta.public === true)
const sidebarOpen = ref(true)

interface MenuHandle {
  open: (index: string) => void
}
const menuRef = ref<MenuHandle | null>(null)

// Element Plus only applies default-active on mount; open the cluster/node
// branch explicitly so the current node stays visible after navigation.
watch(
  () => route.fullPath,
  () => {
    const cluster = String(route.params.cluster ?? '')
    if (!cluster) {
      return
    }
    menuRef.value?.open(`/clusters/${cluster}/group`)
    if (route.name === 'node-detail') {
      menuRef.value?.open(`/clusters/${cluster}/members`)
    }
  },
  { immediate: true, flush: 'post' },
)

const pageTitle = computed(() => {
  if (typeof route.meta.titleKey === 'string') {
    return t(route.meta.titleKey)
  }
  if (typeof route.meta.title === 'string') {
    return route.meta.title
  }

  return t('app.brand')
})
const elementLocale = computed(() => (locale.value === 'zh-CN' ? zhCn : en))

const activeMenu = computed(() => {
  if (route.name === 'node-detail') {
    return `/clusters/${String(route.params.cluster ?? '')}/members/${String(route.params.member ?? '')}`
  }

  // A deep link may arrive with a trailing slash; menu indexes never have one,
  // so normalise before matching or the active highlight silently disappears.
  const path = route.path
  return path.length > 1 ? path.replace(/\/+$/, '') : path
})

interface Crumb {
  title: string
  to?: string
}

/** Breadcrumb makes the Fleet → cluster → node hierarchy explicit. */
const breadcrumbs = computed<Crumb[]>(() => {
  const crumbs: Crumb[] = []

  if (route.name === 'cluster' || route.name === 'node-detail') {
    const clusterName = String(route.params.cluster ?? '')
    const cluster = clusters.byName(clusterName)
    crumbs.push({ title: t('nav.clusters'), to: '/' })
    crumbs.push({
      title: cluster?.display_name || clusterName,
      to: route.name === 'node-detail' ? `/clusters/${clusterName}` : undefined,
    })
    if (route.name === 'node-detail') {
      crumbs.push({ title: String(route.params.member ?? '') })
    }
    return crumbs
  }

  crumbs.push({ title: pageTitle.value })
  return crumbs
})

watch(
  () => auth.authenticated,
  (authenticated, previous) => {
    if (!authenticated) {
      // Before the router guard has resolved the initial URL, the current
      // route is still START_LOCATION ("/"): redirecting here would replace a
      // deep link with the dashboard. Only a genuine mid-page 401, after the
      // session was established, needs the fallback.
      if (!auth.initialized || previous !== true) {
        return
      }

      if (route.meta.public !== true) {
        void router.push({
          name: 'login',
          query: route.fullPath === '/' ? {} : { redirect: route.fullPath },
        })
      }

      return
    }

    if (clusters.items.length === 0) {
      void clusters.fetchAll()
    }
    if (auth.canAdmin && !plugins.loaded) {
      void plugins.fetch()
    }
  },
  { immediate: true },
)

watch(
  [pageTitle, locale],
  () => {
    document.title = `${pageTitle.value} · ${t('app.brand')}`
  },
  { immediate: true },
)

async function logout(): Promise<void> {
  await auth.logout()
  await router.push({ name: 'login' })
}
</script>

<template>
  <el-config-provider :locale="elementLocale">
    <router-view v-if="publicPage" />

    <el-container v-else class="app-shell" :class="{ 'sidebar-collapsed': !sidebarOpen }">
      <el-aside width="236px" class="sidebar">
        <router-link to="/" class="brand">
          <span class="brand-mark">IF</span>
          <span class="brand-text">
            <strong>{{ t('app.brand') }}</strong>
            <small>{{ t('app.tagline') }}</small>
          </span>
        </router-link>

        <el-menu ref="menuRef" :default-active="activeMenu" router class="nav-menu">
          <div class="nav-group-label">{{ t('nav.workbench') }}</div>

          <el-menu-item index="/">
            <el-icon><Odometer /></el-icon>
            <span>{{ t('nav.dashboard') }}</span>
          </el-menu-item>

          <el-menu-item index="/instances">
            <el-icon><Box /></el-icon>
            <span>{{ t('nav.instances') }}</span>
          </el-menu-item>

          <el-menu-item index="/volumes">
            <el-icon><Coin /></el-icon>
            <span>{{ t('nav.fleetVolumes') }}</span>
          </el-menu-item>

          <div class="nav-group-label">{{ t('nav.resources') }}</div>

          <el-menu-item index="/assets">
            <el-icon><Collection /></el-icon>
            <span>{{ t('nav.assets') }}</span>
          </el-menu-item>

          <el-menu-item v-if="auth.canAdmin" index="/plugins">
            <el-icon><Grid /></el-icon>
            <span>{{ t('nav.plugins') }}</span>
            <span v-if="plugins.count > 0" class="nav-count">{{ plugins.count }}</span>
          </el-menu-item>

          <div class="nav-group-label">{{ t('nav.clusters') }}</div>

          <el-sub-menu
            v-for="cluster in clusters.items"
            :key="cluster.name"
            :index="`/clusters/${cluster.name}/group`"
          >
            <template #title>
              <span class="health-dot" :class="{ online: cluster.online }" />
              <span>{{ cluster.display_name }}</span>
            </template>
            <el-menu-item :index="`/clusters/${cluster.name}`">
              {{ t('cluster.overview') }}
            </el-menu-item>
            <el-sub-menu :index="`/clusters/${cluster.name}/members`">
              <template #title>{{ t('nav.nodes') }}</template>
              <el-menu-item
                v-for="member in cluster.members ?? []"
                :key="member.name"
                :index="`/clusters/${cluster.name}/members/${member.name}`"
              >
                <span class="health-dot" :class="{ online: member.status === 'Online' }" />
                <span>{{ member.name }}</span>
              </el-menu-item>
            </el-sub-menu>
          </el-sub-menu>

          <el-menu-item v-if="clusters.items.length === 0" index="/clusters-empty" disabled>
            <span>{{ t('app.noClusters') }}</span>
          </el-menu-item>

          <template v-if="auth.canAdmin">
            <div class="nav-group-label">{{ t('nav.admin') }}</div>
            <el-menu-item index="/users">
              <el-icon><UserFilled /></el-icon>
              <span>{{ t('nav.users') }}</span>
            </el-menu-item>
          </template>
        </el-menu>

        <div class="sidebar-footer">
          <span class="health-dot" :class="{ online: clusters.onlineCount > 0 }" />
          <span>
            {{ t('app.clustersOnline', { online: clusters.onlineCount, total: clusters.items.length }) }}
          </span>
        </div>
      </el-aside>

      <el-container class="main-column">
        <el-header class="topbar">
          <div class="topbar-heading">
            <el-button
              class="mobile-menu-toggle"
              text
              :icon="sidebarOpen ? Fold : Expand"
              :aria-label="t('nav.clusters')"
              @click="sidebarOpen = !sidebarOpen"
            />
            <div>
              <el-breadcrumb class="breadcrumbs" separator="/">
                <el-breadcrumb-item v-for="(crumb, index) in breadcrumbs" :key="index">
                  <router-link v-if="crumb.to" :to="crumb.to">{{ crumb.title }}</router-link>
                  <span v-else>{{ crumb.title }}</span>
                </el-breadcrumb-item>
              </el-breadcrumb>
              <h1>{{ pageTitle }}</h1>
              <p>{{ t('app.subtitle') }}</p>
            </div>
          </div>
          <div class="topbar-actions">
            <span v-if="auth.username" class="topbar-user">{{ auth.username }}</span>
            <LocaleSwitch />
            <el-button :icon="SwitchButton" plain @click="logout">
              {{ t('app.signOut') }}
            </el-button>
          </div>
        </el-header>

        <el-main class="content">
          <router-view />
        </el-main>
      </el-container>
    </el-container>
  </el-config-provider>
</template>
