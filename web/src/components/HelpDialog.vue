<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { currentLocale, setLocale } from '@/i18n'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: boolean): void }>()

const { t } = useI18n()

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const currentDoc = ref('overview')

/** Documentation sections per locale, keyed by doc id. */
const docs = computed<Record<string, Record<string, string>>>(() => ({
  overview: {
    en: `
      <h3>Overview</h3>
      <p>Incus Fleet is a single control plane for many independent Incus clusters.
      Register clusters with an admin token, then manage everything from one web UI.</p>
      <ul>
        <li><strong>Clusters</strong> — health, members, storage pools, networks, profiles.</li>
        <li><strong>Instances</strong> — create, snapshot, clone, rebuild, migrate, delete.</li>
        <li><strong>Browser sessions</strong> — exec terminals, serial consoles, SPICE desktops.</li>
        <li><strong>Assets</strong> — shared SSH keys, scripts, images, and ISOs across clusters.</li>
        <li><strong>Monitoring</strong> — CPU/memory/disk/network for clusters, nodes, instances.</li>
      </ul>
    `,
    zh: `
      <h3>概览</h3>
      <p>Incus Fleet 是面向多个独立 Incus 集群的统一控制平面。
      使用管理员 token 注册集群后,即可在一个 Web 界面中管理全部资源。</p>
      <ul>
        <li><strong>集群</strong> — 健康状态、成员、存储池、网络、配置文件。</li>
        <li><strong>实例</strong> — 创建、快照、克隆、重建、迁移、删除。</li>
        <li><strong>浏览器会话</strong> — 命令行终端、串行控制台、SPICE 远程桌面。</li>
        <li><strong>资产库</strong> — 跨集群共享的 SSH 公钥、脚本、镜像与 ISO。</li>
        <li><strong>监控</strong> — 集群/节点/实例的 CPU、内存、磁盘、网络指标。</li>
      </ul>
    `,
  },
  deployment: {
    en: `
      <h3>Deployment</h3>
      <p>Fleet ships as a single Go binary with the UI embedded.</p>
      <ol>
        <li>Download the binary from GitHub Releases (amd64 or arm64).</li>
        <li>Create a configuration file — see <code>deploy/config.example.yaml</code>.</li>
        <li>Install the systemd unit from <code>deploy/fleet.service</code>.</li>
        <li>Open the web port and sign in with the admin token.</li>
        <li>Optional: terminate TLS with the bundled Caddy example.</li>
      </ol>
      <p>Full details: <code>docs/deployment.md</code>.</p>
    `,
    zh: `
      <h3>部署</h3>
      <p>Fleet 以单个 Go 二进制发布,UI 已内嵌。</p>
      <ol>
        <li>从 GitHub Releases 下载二进制(amd64 或 arm64)。</li>
        <li>创建配置文件 — 参考 <code>deploy/config.example.yaml</code>。</li>
        <li>安装 <code>deploy/fleet.service</code> 提供的 systemd 单元。</li>
        <li>打开 Web 端口,使用管理员 token 登录。</li>
        <li>可选:使用仓库内 Caddy 示例终止 TLS。</li>
      </ol>
      <p>完整说明见 <code>docs/deployment.md</code>。</p>
    `,
  },
  features: {
    en: `
      <h3>Feature guide</h3>
      <ul>
        <li><strong>ISO install</strong> — upload an installer ISO on the cluster page, launch a
        blank VM from it, open the console and finish the OS install.</li>
        <li><strong>Images &amp; ISO library</strong> — the Assets page lists images and ISOs from
        every cluster and copies them between clusters.</li>
        <li><strong>Background uploads</strong> — large uploads minimize to the corner with
        progress, speed, and ETA; a toast appears when finished.</li>
        <li><strong>Node networks</strong> — edit NICs, bridges, DNS, routes on each member.</li>
        <li><strong>Node upgrades</strong> — trigger Incus upgrades per member from the cluster page.</li>
      </ul>
    `,
    zh: `
      <h3>功能指南</h3>
      <ul>
        <li><strong>ISO 安装系统</strong> — 在集群页上传安装 ISO,从它启动空白虚拟机,
        打开控制台完成操作系统安装。</li>
        <li><strong>镜像与 ISO 库</strong> — 资产库页汇总所有集群的镜像与 ISO,并可跨集群复制。</li>
        <li><strong>后台上传</strong> — 大文件上传可最小化到角落,显示进度、速度与预计时间,
        完成后右下角弹窗提示。</li>
        <li><strong>节点网络</strong> — 在每个成员上编辑网卡、网桥、DNS、路由。</li>
        <li><strong>节点升级</strong> — 在集群页对单个成员触发 Incus 升级。</li>
      </ul>
    `,
  },
}))

const currentContent = computed(() => docs.value[currentDoc.value]?.[localeLabel.value] ?? '')

const localeLabel = computed(() => (lang.value === 'zh-CN' ? 'zh' : 'en'))

const lang = ref(currentLocale())

watch(lang, (value) => setLocale(value))
</script>

<template>
  <el-dialog v-model="visible" :title="t('help.title')" width="760px" destroy-on-close>
    <div class="help-layout">
      <el-menu :default-active="currentDoc" class="help-menu" @select="(val: string) => (currentDoc = val)">
        <el-menu-item index="overview">{{ t('help.overview') }}</el-menu-item>
        <el-menu-item index="deployment">{{ t('help.deployment') }}</el-menu-item>
        <el-menu-item index="features">{{ t('help.features') }}</el-menu-item>
      </el-menu>

      <div class="doc-content">
        <!-- Documentation content is static, authored in this repository. -->
        <div v-html="currentContent"></div>
      </div>
    </div>

    <template #footer>
      <div class="help-footer">
        <span class="help-lang">{{ t('app.language') }}</span>
        <el-select v-model="lang" size="small" style="width: 120px">
          <el-option label="中文" value="zh-CN" />
          <el-option label="English" value="en" />
        </el-select>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.help-layout {
  display: flex;
  gap: 16px;
  min-height: 320px;
}

.help-menu {
  width: 180px;
  flex-shrink: 0;
}

.doc-content {
  flex: 1;
  overflow-y: auto;
  max-height: 420px;
  padding: 4px 12px;
  line-height: 1.6;
}

.doc-content :deep(h3) {
  margin-top: 0;
  font-size: 16px;
}

.doc-content :deep(code) {
  background: var(--el-fill-color-light);
  padding: 1px 5px;
  border-radius: 3px;
  font-size: 12px;
}

.help-footer {
  display: flex;
  align-items: center;
  gap: 12px;
  justify-content: flex-end;
}

.help-lang {
  color: var(--fleet-ink-soft, var(--el-text-color-secondary));
  font-size: 13px;
}
</style>
