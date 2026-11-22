<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  Box,
  Connection,
  Odometer,
  Switch as SwitchIcon,
  SwitchButton,
  Collection,
  Document,
  UserFilled,
  Coin,
} from '@element-plus/icons-vue'
import en from 'element-plus/es/locale/lang/en'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

import LocaleSwitch from '@/components/LocaleSwitch.vue'
import { useAuthStore } from '@/stores/auth'
import { useClustersStore } from '@/stores/clusters'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const clusters = useClustersStore()
const { t, locale } = useI18n()

const publicPage = computed(() => route.meta.public === true)
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
  if (route.name === 'cluster' || route.name === 'node-detail') {
    return `/clusters/${String(route.params.cluster ?? '')}`
  }

  return route.path
})

watch(
  () => auth.authenticated,
  (authenticated) => {
    if (!authenticated) {
      // A 401 mid-page used to leave the user stranded with failing
      // requests; route to the sign-in view and offer to come back (#12).
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

    <el-container v-else class="app-shell">
      <el-aside width="236px" class="sidebar">
        <router-link to="/" class="brand">
          <span class="brand-mark">IF</span>
          <span class="brand-text">
            <strong>{{ t('app.brand') }}</strong>
            <small>{{ t('app.tagline') }}</small>
          </span>
        </router-link>

        <el-menu :default-active="activeMenu" router class="nav-menu">
          <el-menu-item index="/">
            <el-icon><Odometer /></el-icon>
            <span>{{ t('nav.dashboard') }}</span>
          </el-menu-item>

          <el-sub-menu index="/clusters">
            <template #title>
              <el-icon><Connection /></el-icon>
              <span>{{ t('nav.clusters') }}</span>
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
              <span>{{ t('app.noClusters') }}</span>
            </el-menu-item>
          </el-sub-menu>

          <el-menu-item index="/instances">
            <el-icon><Box /></el-icon>
            <span>{{ t('nav.instances') }}</span>
          </el-menu-item>

          <el-menu-item index="/volumes">
            <el-icon><Coin /></el-icon>
            <span>{{ t('nav.fleetVolumes') }}</span>
          </el-menu-item>

          <el-menu-item index="/migrations">
            <el-icon><SwitchIcon /></el-icon>
            <span>{{ t('nav.migrations') }}</span>
          </el-menu-item>

          <el-menu-item index="/assets">
            <el-icon><Collection /></el-icon>
            <span>{{ t('nav.assets') }}</span>
          </el-menu-item>

          <el-menu-item index="/answer-profiles">
            <el-icon><Document /></el-icon>
            <span>{{ t('nav.answerProfiles') }}</span>
          </el-menu-item>

          <el-menu-item v-if="auth.canAdmin" index="/users">
            <el-icon><UserFilled /></el-icon>
            <span>{{ t('nav.users') }}</span>
          </el-menu-item>
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
          <div>
            <h1>{{ pageTitle }}</h1>
            <p>{{ t('app.subtitle') }}</p>
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
