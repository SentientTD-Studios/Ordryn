import { computed, ref, watch, type MaybeRefOrGetter, toValue } from 'vue'
import { api } from '@/api/client'
import type { CustomFieldDef } from '@/api/types'

const cache = new Map<number, CustomFieldDef[]>()
const inflight = new Map<number, Promise<CustomFieldDef[]>>()
const cacheEpoch = ref(0)

export function clearCustomFieldDefsCache(projectId?: number) {
  if (projectId) {
    cache.delete(projectId)
    inflight.delete(projectId)
  } else {
    cache.clear()
    inflight.clear()
  }
  cacheEpoch.value++
}

export async function loadCustomFieldDefs(projectId: number): Promise<CustomFieldDef[]> {
  if (cache.has(projectId)) return cache.get(projectId) || []
  const pending = inflight.get(projectId)
  if (pending) return pending
  const epoch = cacheEpoch.value
  const req = api
    .listProjectCustomFields(projectId)
    .then((res) => {
      const fields = res.fields || []
      if (epoch === cacheEpoch.value) cache.set(projectId, fields)
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
  let seq = 0

  watch(
    () => [toValue(projectId), cacheEpoch.value] as const,
    async ([id]) => {
      const n = ++seq
      if (!id) {
        defs.value = []
        return
      }
      try {
        const next = await loadCustomFieldDefs(id)
        if (n === seq) defs.value = next
      } catch {
        if (n === seq) defs.value = []
      }
    },
    { immediate: true },
  )

  return { defs, defByKey }
}
