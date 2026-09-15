# Severity

Manifest-only custom field. Registers `severity.level` (enum) on project tasks. Core renders the sidebar dropdown and kanban badge; this folder has no `ui.js`.

## Install

1. Copy this directory to `data/extensions/severity` (folder name must stay `severity`).
2. Restart Ordryn.
3. Site admin: Admin → Extensions → Severity → Enable, then Save.
4. Project owner: Project settings → Extensions → Severity → Enable for this project, Save.

Open a project task and set Severity. The value appears on kanban cards without a card fork.
