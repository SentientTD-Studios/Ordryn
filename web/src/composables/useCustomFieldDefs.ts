import { computed, ref, watch, type MaybeRefOrGetter, toValue } from 'vue'
import { api } from '@/api/client'
import type { CustomFieldDef } from '@/api/types'

const cache = new Map<number, CustomFieldDef[]>()
const inflight = new Map<number, Promise<CustomFieldDef[]>>()

export function clearCustomFieldDefsCache(projectId?: number) {
  if (projectId) cache.delete(projectId)
  else cache.clear()
}

export async function loadCustomFieldDefs(projectId: number): Promise<CustomFieldDef[]> {
  if (cache.has(projectId)) return cache.get(projectId) || []
  const pending = inflight.get(projectId)
  if (pending) return pending
  const req = api
    .listProjectCustomFields(projectId)
    .then((res) => {
      const fields = res.fields || []
      cache.set(projectId, fields)
      return fields
    })
    .finally(() => {
      inflight.delete(projectId)
    })
  inflight.set(projectId, req)
  return req
}

export function useCustomFieldDefs(projectId: MaybeRefOrGetter<number | null | undefined>) {
  const defs = ref<CustomFieldDef[]>([])
  const defByKey = computed(() => {
    const map: Record<string, CustomFieldDef> = {}
    for (const d of defs.value) map[d.field_key] = d
    return map
  })

  watch(
    () => toValue(projectId),
    async (id) => {
      if (!id) {
        defs.value = []
        return
      }
      try {
        defs.value = await loadCustomFieldDefs(id)
      } catch {
        defs.value = []
      }
    },
    { immediate: true },
  )

  return { defs, defByKey }
}
