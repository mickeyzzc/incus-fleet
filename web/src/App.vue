<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
import { mergePluginMessages } from '@/plugins/messages'
import { registerPluginRoutes } from '@/router'
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
const mobileQuery = typeof window !== 'undefined' && typeof window.matchMedia === 'function'
  ? window.matchMedia('(max-width: 960px)')
  : null
const isMobile = ref(mobileQuery?.matches ?? false)
const sidebarOpen = ref(!isMobile.value)
const SIDEBAR_COLLAPSED_KEY = 'incus-fleet.sidebar.collapsed'
const isCollapsed = ref(localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === '1')

watch(isCollapsed, (value) => {
  localStorage.setItem(SIDEBAR_COLLAPSED_KEY, value ? '1' : '0')
})

interface MenuHandle {
  open: (index: string) => void
}
const menuRef = ref<MenuHandle | null>(null)

/**
 * Expand one sidebar branch. Element Plus registers sub-menus during render,
 * so on a cold deep link the index can be missing for a moment; its own
 * defaultActive handling opens the branch in that case.
 */
function openMenuBranch(index: string): void {
  try {
    menuRef.value?.open(index)
  } catch {
    // Branch not mounted yet - defaultActive will reveal it once it is.
  }
}

// Element Plus only applies default-active on mount; open the cluster/node
// branch explicitly so the current node stays visible after navigation.
watch(
  [() => route.fullPath, () => clusters.items],
  () => {
    const cluster = String(route.params.cluster ?? '')
    // A deep link can render before the cluster list arrives. Element Plus
    // throws when asked to open a sub-menu whose index is not registered yet.
    if (!cluster || !clusters.items.some((item) => item.name === cluster)) {
      return
    }
    openMenuBranch(`/clusters/${cluster}/group`)
    if (route.name === 'node-detail') {
      openMenuBranch(`/clusters/${cluster}/members`)
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

// Installed plugins contribute data, not sidebar items. Their same-origin UI
// routes and translated titles are registered before the catch-all route.
watch(
  () => plugins.navigation,
  (entries) => registerPluginRoutes(router, entries),
  { immediate: true },
)

watch(
  () => plugins.items,
  (items) => {
    for (const entry of items) {
      mergePluginMessages(entry.id, entry.ui.messages)
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

function onViewportChange(event: MediaQueryListEvent): void {
  isMobile.value = event.matches
  sidebarOpen.value = !event.matches
}

function closeMobileSidebar(): void {
  if (isMobile.value) {
    sidebarOpen.value = false
  }
}

// el-table renders header/cell text independently, so the narrow-screen card
// layout needs the column label copied onto each body cell. A single observer
// keeps every table decorated as rows are polled or replaced.
let tableObserver: MutationObserver | null = null
let decorateScheduled = false

function decorateTableCards(): void {
  document.querySelectorAll('.el-table').forEach((table) => {
    const headers = Array.from(table.querySelectorAll('.el-table__header-wrapper th'))
      .map((cell) => cell.textContent?.trim() ?? '')
    table.querySelectorAll('.el-table__body-wrapper tbody tr').forEach((row) => {
      Array.from(row.querySelectorAll('td')).forEach((cell, index) => {
        const label = headers[index]
        if (label) {
          cell.setAttribute('data-label', label)
        }
      })
    })
  })
}

function scheduleTableDecoration(): void {
  if (decorateScheduled) {
    return
  }
  decorateScheduled = true
  const run = (): void => {
    decorateScheduled = false
    decorateTableCards()
  }
  if (typeof requestAnimationFrame === 'function') {
    requestAnimationFrame(run)
  } else {
    run()
  }
}

onMounted(() => {
  mobileQuery?.addEventListener('change', onViewportChange)
  if (typeof MutationObserver !== 'undefined') {
    tableObserver = new MutationObserver(scheduleTableDecoration)
    tableObserver.observe(document.body, { childList: true, subtree: true })
    scheduleTableDecoration()
  }
})

onBeforeUnmount(() => {
  mobileQuery?.removeEventListener('change', onViewportChange)
  tableObserver?.disconnect()
  tableObserver = null
})

async function logout(): Promise<void> {
  await auth.logout()
  await router.push({ name: 'login' })
}
</script>

<template>
  <el-config-provider :locale="elementLocale">
    <router-view v-if="publicPage" />

    <el-container
      v-else
      class="app-shell"
      :class="{ 'sidebar-collapsed': !sidebarOpen, 'sidebar-icon-only': isCollapsed }"
    >
      <el-aside :width="isCollapsed ? '64px' : '236px'" class="sidebar">
        <router-link to="/" class="brand">
          <span class="brand-mark">IF</span>
          <span class="brand-text">
            <strong>{{ t('app.brand') }}</strong>
            <small>{{ t('app.tagline') }}</small>
          </span>
        </router-link>

        <el-menu
          ref="menuRef"
          :default-active="activeMenu"
          :collapse="isCollapsed"
          :collapse-transition="false"
          router
          class="nav-menu"
          @select="closeMobileSidebar"
        >
          <div v-if="!isCollapsed" class="nav-group-label">{{ t('nav.workbench') }}</div>

          <el-menu-item index="/" :title="t('nav.dashboard')">
            <el-icon><Odometer /></el-icon>
            <span>{{ t('nav.dashboard') }}</span>
          </el-menu-item>

          <el-menu-item index="/instances" :title="t('nav.instances')">
            <el-icon><Box /></el-icon>
            <span>{{ t('nav.instances') }}</span>
          </el-menu-item>

          <el-menu-item index="/volumes" :title="t('nav.fleetVolumes')">
            <el-icon><Coin /></el-icon>
            <span>{{ t('nav.fleetVolumes') }}</span>
          </el-menu-item>

          <div v-if="!isCollapsed" class="nav-group-label">{{ t('nav.resources') }}</div>

          <el-menu-item index="/assets" :title="t('nav.assets')">
            <el-icon><Collection /></el-icon>
            <span>{{ t('nav.assets') }}</span>
          </el-menu-item>

          <el-menu-item v-if="auth.canAdmin" index="/plugins" :title="t('nav.plugins')">
            <el-icon><Grid /></el-icon>
            <span>{{ t('nav.plugins') }}</span>
            <span
              v-if="!isCollapsed && plugins.attentionCount > 0"
              class="nav-badge"
              :title="t('plugins.attention', { count: plugins.attentionCount })"
            >{{ plugins.attentionCount }}</span>
          </el-menu-item>

          <div v-if="!isCollapsed" class="nav-group-label">{{ t('nav.clusters') }}</div>

          <el-sub-menu
            v-for="cluster in clusters.items"
            :key="cluster.name"
            :index="`/clusters/${cluster.name}/group`"
            :title="cluster.display_name"
            :show-timeout="80"
            :hide-timeout="80"
          >
            <template #title>
              <el-icon><Grid /></el-icon>
              <span class="health-dot" :class="{ online: cluster.online }" />
              <span>{{ cluster.display_name }}</span>
            </template>
            <el-menu-item
              v-if="isCollapsed"
              :index="`/clusters/${cluster.name}/label`"
              class="menu-cluster-heading"
              disabled
            >
              {{ cluster.display_name }}
            </el-menu-item>
            <el-menu-item :index="`/clusters/${cluster.name}`" :title="t('cluster.overview')">
              {{ t('cluster.overview') }}
            </el-menu-item>
            <el-sub-menu
              :index="`/clusters/${cluster.name}/members`"
              :title="t('nav.nodes')"
              :show-timeout="80"
              :hide-timeout="80"
            >
              <template #title>{{ t('nav.nodes') }}</template>
              <el-menu-item
                v-for="member in cluster.members ?? []"
                :key="member.name"
                :index="`/clusters/${cluster.name}/members/${member.name}`"
                :title="member.name"
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
            <el-menu-item index="/users" :title="t('nav.users')">
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
      <div v-if="isMobile && sidebarOpen" class="sidebar-backdrop" @click="sidebarOpen = false" />

      <el-container class="main-column">
        <el-header class="topbar">
          <div class="topbar-heading">
            <el-button
              class="sidebar-toggle"
              text
              :icon="isCollapsed ? Expand : Fold"
              :aria-label="isCollapsed ? t('nav.expandSidebar') : t('nav.collapseSidebar')"
              :title="isCollapsed ? t('nav.expandSidebar') : t('nav.collapseSidebar')"
              @click="isCollapsed = !isCollapsed"
            />
            <el-button
              class="mobile-menu-toggle"
              text
              :icon="sidebarOpen ? Fold : Expand"
              :aria-label="t('nav.toggleSidebar')"
              @click="sidebarOpen = !sidebarOpen"
            />
            <el-breadcrumb class="breadcrumbs" separator="/">
              <el-breadcrumb-item v-for="(crumb, index) in breadcrumbs" :key="index">
                <router-link v-if="crumb.to" :to="crumb.to">{{ crumb.title }}</router-link>
                <span v-else>{{ crumb.title }}</span>
              </el-breadcrumb-item>
            </el-breadcrumb>
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
