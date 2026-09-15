# Custom fields demo

Manifest-only catalog of every v1 custom field type: string, number, boolean, enum, url, and user. Core renders the sidebar and badges. Safe to enable alongside `severity` (this enum is Size, not Severity).

## Install

1. Copy this directory to `data/extensions/fields-demo` (folder name must stay `fields-demo`).
2. Restart Ordryn.
3. Site admin: Admin → Extensions → Custom fields demo → Enable, then Save.
4. Project owner: Project settings → Extensions → Custom fields demo → Enable for this project, Save.

Then edit a project task. Ticket and owner show on list rows; score, blocked, and size show on the board; spec stays in the sidebar.
