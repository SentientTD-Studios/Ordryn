import type { ProjectExtension } from '@/api/types'

const DELIVERY_NAMES: Record<string, string> = {
  'discord.webhook': 'Discord',
  'slack.webhook': 'Slack',
  'teams.webhook': 'Teams',
  'googlechat.webhook': 'Google Chat',
  'ntfy.webhook': 'ntfy',
  'http.webhook': 'Webhook',
}

export function isTeamExtensionEnabled(ext: ProjectExtension): boolean {
  return !!ext.site_enabled && !!ext.settings?.enabled
}

export function enabledTeamExtensions(list: ProjectExtension[]): ProjectExtension[] {
  return list.filter(isTeamExtensionEnabled)
}

export function triggerLabels(ext: ProjectExtension): string[] {
  const hooks = ext.manifest.hooks || []
  const triggers = ext.settings?.triggers || []
  if (triggers.includes('*')) {
    return hooks.map((h) => h.label || h.on)
  }
  const byOn = new Map(hooks.map((h) => [h.on, h.label || h.on]))
  return triggers.map((t) => byOn.get(t) || t)
}

export function destinationSite(ext: ProjectExtension): string {
  const type = ext.manifest.delivery?.type || ''
  const name = DELIVERY_NAMES[type] || ''
  const host = (ext.destination_host || '').trim()
  if (host) return name ? `${name} · ${host}` : host
  if (!type) return 'In-app only'
  return name ? `${name} · not configured` : 'Not configured'
}
