# GoTodo — Implementation Roadmap

**Status: Complete (v0.15.1-beta)** — All waves (0–5) and items A1–A13, B1–B16 are implemented.

Post-roadmap polish (dev branch): tag management UI, duplicate task, relative due labels, due-date presets, navbar overdue badge, live description validation, Go test CI, undo delete, ICS calendar feed, import preview, bulk due-date actions, UX polish (loading states, mobile filters, sidebar backdrop).

---

## Implementation Waves

| Wave | Items | Status |
|------|-------|--------|
| **0 — Foundation** | B7, B13, B1, B2, B15, B16 | Done |
| **1 — Quick UX** | B3, B4, B5, B6, B9, B11 | Done |
| **2 — Filters & schema** | A2, A3, B10, filter refactor | Done |
| **3 — Organization** | A1, A9 (full), B8, B14 (partial) | Done |
| **4 — Power tools** | A8, A7 | Done |
| **5 — Polish** | A10, A11, A13, B12, B14 (rest) | Done |

---

## Part A — New Features

| ID | Item | Status |
|----|------|--------|
| A1 | Tags / Labels | Done (create-on-type; tag mgmt on Projects page) |
| A2 | Priority Levels | Done |
| A3 | Smart Views / Quick Filters | Done |
| A7 | Export / Import | Done |
| A8 | Bulk Actions | Done |
| A9 | Richer Descriptions / Notes | Done (Markdown) |
| A10 | Keyboard Shortcuts | Done |
| A11 | Task Activity / Audit Trail | Done |
| A13 | Dashboard / Insights | Done |

---

## Part B — Cleanup & UX

| ID | Item | Status |
|----|------|--------|
| B1 | Fix search pagination bug | Done |
| B2 | Fix post-delete page detection | Done |
| B3 | Show project name on task rows | Done |
| B4 | Project rename UI | Done |
| B5 | Allow editing completed tasks | Done |
| B6 | Increase description limit | Done |
| B7 | HTMX DOM deduplication | Done |
| B8 | Wire expand CSS | Done |
| B9 | Due date visual indicators | Done |
| B10 | Favorites + pagination clarity | Done |
| B11 | Project filter reset on create | Done |
| B12 | Consolidate admin/moderation UX | Done |
| B13 | Dead code / hygiene | Done |
| B14 | Accessibility improvements | Done (incremental) |
| B15 | get-next-item removal | Done |
| B16 | Task count query performance | Done |

---

## Resolved Decisions

| Decision | Resolution |
|----------|------------|
| A9 depth | Full Markdown |
| A7 import | Auto-create projects and tags |
| B10 favorites | Excluded from pagination count |
| B15 delete | Full list refresh |
| A1 tags | Create-on-type in sidebar |

---

## Shipped post-v0.15.1

| Feature | Notes |
|---------|-------|
| Undo delete | Session snapshot, ~60s toast on home page |
| ICS calendar feed | Profile page subscribe URL; public `/cal/{token}.ics` |
| Import preview | Upload CSV → preview → confirm/cancel |
| Bulk set/clear due date | Bulk action bar |

---

## Future Ideas (not in scope)

See project planning notes for Tier 2+ enhancements: saved filter views, JSON import, email digest.
