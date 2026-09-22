export type ExtensionSettingScope = 'site' | 'project' | 'kanban' | 'user' | 'member' | string

export function settingScopes(field: { scope?: string | string[] }): string[] {
  if (field.scope == null || field.scope === '') return ['site']
  const raw = Array.isArray(field.scope) ? field.scope : [field.scope]
  const out: string[] = []
  for (const item of raw) {
    const n = String(item || '')
      .trim()
      .toLowerCase()
    if (!n) continue
    const canon = n === 'member' ? 'user' : n
    if (!out.includes(canon)) out.push(canon)
  }
  return out.length ? out : ['site']
}

export function settingHasScope(field: { scope?: string | string[] }, scope: string): boolean {
  const want = scope === 'member' ? 'user' : String(scope || '').trim().toLowerCase()
  return settingScopes(field).includes(want)
}
