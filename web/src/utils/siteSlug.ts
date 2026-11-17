// Names derived from the site name (Admin → Site settings), so keys, MCP servers,
// and environment variables carry the site's own branding.

export const DEFAULT_SITE_SLUG = 'ordryn'

/** Lowercase letters, digits, and single dashes, at most 24 characters; "ordryn" when nothing is left. */
export function siteSlug(siteName?: string | null): string {
  const slug = (siteName ?? '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 24)
    .replace(/-+$/g, '')
  return slug || DEFAULT_SITE_SLUG
}

/** Environment variable for a key, e.g. "Acme Tasks" → ACME_TASKS_API_KEY. */
export function siteKeyEnvVar(siteName?: string | null): string {
  return `${siteSlug(siteName).toUpperCase().replace(/-/g, '_')}_API_KEY`
}
