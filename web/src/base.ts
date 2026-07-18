declare global {
  interface Window {
    __GOTODO_BASE__?: string
  }
}

/** Trailing-slash SPA mount (e.g. "/" or "/gotodo/"). */
export function appBase(): string {
  const meta = document.querySelector('meta[name="gotodo-base"]')?.getAttribute('content')
  if (meta && meta.length > 0) {
    return meta.endsWith('/') ? meta : `${meta}/`
  }
  // Legacy fallback if an older server inject is present.
  const injected = window.__GOTODO_BASE__
  if (typeof injected === 'string' && injected.length > 0) {
    return injected.endsWith('/') ? injected : `${injected}/`
  }
  const env = import.meta.env.BASE_URL || '/'
  if (env === './') return '/'
  return env.endsWith('/') ? env : `${env}/`
}

/** Path prefix without trailing slash ("" at site root, "/gotodo" under a subpath). */
export function pathPrefix(): string {
  const base = appBase().replace(/\/$/, '')
  return base === '' ? '' : base
}

/** Prefix a root-absolute API/site path with the deploy prefix. */
export function withBase(path: string): string {
  if (!path) return pathPrefix() || '/'
  if (/^https?:\/\//i.test(path) || path.startsWith('//')) return path
  const normalized = path.startsWith('/') ? path : `/${path}`
  const prefix = pathPrefix()
  if (!prefix) return normalized
  if (normalized === prefix || normalized.startsWith(`${prefix}/`)) return normalized
  return `${prefix}${normalized}`
}
