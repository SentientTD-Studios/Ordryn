export const PROFILE_SECTIONS = [
  'account',
  'preferences',
  'notifications',
  'integrations',
  'data',
  'developer',
] as const

export type ProfileSection = (typeof PROFILE_SECTIONS)[number]

export const PROFILE_SECTION_ITEMS: { id: ProfileSection; label: string }[] = [
  { id: 'account', label: 'Account' },
  { id: 'preferences', label: 'Preferences' },
  { id: 'notifications', label: 'Notifications' },
  { id: 'integrations', label: 'Integrations' },
  { id: 'data', label: 'Data' },
  { id: 'developer', label: 'Developer' },
]

export const PROFILE_SECTION_ALIASES: Record<string, ProfileSection> = {
  'github-section': 'integrations',
  'calendar-feed': 'integrations',
  'api-keys-section': 'developer',
  'mfa-section': 'account',
}

function hashId(hash: string): string {
  return hash.replace(/^#/, '').trim()
}

export function isProfileSectionHash(hash: string): boolean {
  const id = hashId(hash)
  if (!id) return true
  return (PROFILE_SECTIONS as readonly string[]).includes(id) || id in PROFILE_SECTION_ALIASES
}

export function resolveProfileSection(hash: string, githubQuery?: string): ProfileSection {
  if (githubQuery === 'connected' || githubQuery === 'error') return 'integrations'
  const id = hashId(hash)
  if ((PROFILE_SECTIONS as readonly string[]).includes(id)) return id as ProfileSection
  if (id in PROFILE_SECTION_ALIASES) return PROFILE_SECTION_ALIASES[id]
  return 'account'
}
