import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { SiteInfo } from '@/api/types'

const siteInfo = ref<SiteInfo | null>(null)
const loaded = ref(false)
const siteName = computed(() => siteInfo.value?.site_name?.trim() || 'GoTodo')

function applyDocumentTitle(name: string) {
  if (typeof document !== 'undefined') {
    document.title = name || 'GoTodo'
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
