/** How a person reads to others: full name (company mode), else username, else email. */
export function personLabel(p: { full_name?: string | null; user_name?: string | null; email?: string | null }): string {
  return p.full_name?.trim() || p.user_name?.trim() || p.email?.trim() || ''
}
