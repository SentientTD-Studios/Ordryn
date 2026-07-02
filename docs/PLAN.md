---
name: GoTodo Feature Roadmap
overview: All roadmap items (A1–A13, B1–B16) implemented at v0.15.1-beta. Post-roadmap polish completed on dev branch.
todos:
  - id: wave0-foundation
    content: "Wave 0: B7 HTMX dedup, B13 hygiene, B1/B2/B15/B16 bugs"
    status: completed
  - id: wave1-quick-ux
    content: "Wave 1: B3–B6, B9, B11 quick UX fixes"
    status: completed
  - id: wave2-filters-schema
    content: "Wave 2: A2 priority, A3 smart views, B10 favorites clarity"
    status: completed
  - id: wave3-tags-descriptions
    content: "Wave 3: A1 tags, A9 rich descriptions, B8 expand rows"
    status: completed
  - id: wave4-bulk-data
    content: "Wave 4: A8 bulk actions, A7 export/import"
    status: completed
  - id: wave5-polish
    content: "Wave 5: A10 keyboard, A11 audit trail, A13 dashboard, B12/B14"
    status: completed
  - id: post-roadmap-polish
    content: "Post-roadmap: tag UI, duplicate task, due labels/presets, navbar badge, tests CI"
    status: completed
isProject: false
---

# GoTodo — Implementation Plan

**Status: Complete.** All waves shipped in v0.15.1-beta. See [ROADMAP.md](ROADMAP.md) for the summary status table.

The detailed per-item implementation notes below remain as historical reference for how each feature was designed.

---

# Part A — New Features (Detailed)

[Original detailed specs retained for reference — all items implemented.]

## A1. Tags / Labels — Done

Create-on-type via sidebar `new_tags` field. Tag rename/delete on Projects page. Toolbar filter and bulk tag actions.

## A2. Priority Levels — Done

## A3. Smart Views — Done

## A7. Export / Import — Done

## A8. Bulk Actions — Done

## A9. Richer Descriptions — Done (Markdown)

## A10. Keyboard Shortcuts — Done

## A11. Task Activity / Audit Trail — Done

## A13. Dashboard — Done

---

# Part B — Cleanup & UX (Detailed)

All B1–B16 items complete. See [ROADMAP.md](ROADMAP.md).

---

# Testing

- Go unit tests: `due_filters`, `DueDateDisplay`, export/import helpers, markdown, list filters (embedded Postgres)
- CI: `.github/workflows/go-test.yml` runs `go test ./...` on push/PR to `main` and `dev`
