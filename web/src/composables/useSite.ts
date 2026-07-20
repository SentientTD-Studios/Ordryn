import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { SiteInfo } from '@/api/types'

const FALLBACK_SITE_NAME = 'GoTodo'

function readBootstrappedSiteName(): string {
  if (typeof document === 'undefined') return FALLBACK_SITE_NAME
  const meta = document.querySelector('meta[name="gotodo-site-name"]')
  const fromMeta = meta?.getAttribute('content')?.trim()
  if (fromMeta) return fromMeta
  const fromTitle = document.title?.trim()
  if (fromTitle) return fromTitle
  return FALLBACK_SITE_NAME
}

const bootstrappedSiteName = readBootstrappedSiteName()
const siteInfo = ref<SiteInfo | null>(null)
const loaded = ref(false)
const siteName = computed(() => siteInfo.value?.site_name?.trim() || bootstrappedSiteName)

function applyDocumentTitle(name: string) {
  if (typeof document !== 'undefined' && name) {
    document.title = name
  }
}

watch(siteName, (name) => applyDocumentTitle(name))

export function useSite() {
  async function refresh() {
    try {
      siteInfo.value = await api.site()
    } catch {
      siteInfo.value = null
    } finally {
      loaded.value = true
      applyDocumentTitle(siteName.value)
    }
  }

  return {
    siteInfo,
    siteName,
    loaded,
    refresh,
  }
}
