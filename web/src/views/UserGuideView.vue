<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { RouterLink } from 'vue-router'
import { useSite } from '@/composables/useSite'

const { siteName, refresh: refreshSite } = useSite()

onMounted(() => {
  document.body.classList.add('user-guide-page')
  void refreshSite()
})

onUnmounted(() => {
  document.body.classList.remove('user-guide-page')
})
</script>

<template>
  <div class="container mt-3 mb-4">
    <div class="card">
      <div class="card-body">
        <h1 class="card-title">How to use {{ siteName }}</h1>
        <p class="lead">
          A short guide to managing tasks, projects, and views — plus keyboard shortcuts for power
          users.
        </p>

        <nav class="api-docs-toc mb-4" aria-label="Guide sections">
          <ul class="list-inline mb-0">
            <li class="list-inline-item"><a href="#getting-started">Getting started</a></li>
            <li class="list-inline-item"><a href="#tasks">Tasks</a></li>
            <li class="list-inline-item"><a href="#projects-views">Projects &amp; views</a></li>
            <li class="list-inline-item"><a href="#calendar-dashboard">Calendar &amp; dashboard</a></li>
            <li class="list-inline-item"><a href="#collaboration">Collaboration</a></li>
            <li class="list-inline-item"><a href="#shortcuts">Shortcuts</a></li>
            <li class="list-inline-item"><a href="#settings-api">Settings &amp; API</a></li>
          </ul>
        </nav>

        <h2 id="getting-started" class="h4 mt-4">Getting started</h2>
        <p>
          After you sign in, the home page shows your task list. Use the sidebar to switch between
          Home, Dashboard, Calendar, Projects, and saved Views. The header has profile settings,
          theme controls, and links to this guide.
        </p>

        <h2 id="tasks" class="h4 mt-4">Creating and editing tasks</h2>
        <p>
          Click <strong>Add Task</strong> (or press <kbd>n</kbd> on the home page) to open the task
          sidebar. Fill in a title, optional markdown description, due date, project, tags, and priority, then
          save. When an admin has enabled image hosting, paste or drop a JPEG, PNG, GIF, or WebP
          into the description or a comment, or use <strong>Insert image</strong>. The picture
          shows inline in the task description and in discussion comments, sized to the panel.
          Click <strong>Edit</strong> on the description to change the text. Clicking an image
          opens it in a new tab.
        </p>
        <ul>
          <li>Click a task’s edit control (or press <kbd>e</kbd> / <kbd>Enter</kbd> on a focused task) to edit it in the sidebar.</li>
          <li>Use the checkmark to mark a task complete, or press <kbd>x</kbd> on the focused task.</li>
          <li>Nest work under a parent with <strong>Add subtask</strong>; expand or collapse children as needed.</li>
          <li>
            Each task has a stable number (for example <code>#42</code>) shown in the task panel.
            Use <strong>Copy link</strong> to share a URL such as <code>/tasks/42</code>, or search
            for <code>42</code> / <code>#42</code> to find it.
          </li>
          <li>
            Archive a task to hide it from boards and lists. Filter by the <strong>archived</strong> tag
            to find archived work and restore it. Delete is permanent and cannot be undone (a short
            undo token may still appear after delete as a safety net).
          </li>
          <li>Deleting a task may offer undo for a short time when the server returns an undo token.</li>
        </ul>

        <h2 id="projects-views" class="h4 mt-4">Projects, tags, filters, and saved views</h2>
        <p>
          Organize work with projects and tags. On the home page, the filter bar lets you search,
          filter by status / tag / priority / due date, and change sort order or list density.
        </p>
        <ul>
          <li>
            Manage projects from
            <RouterLink to="/projects">Projects</RouterLink>
            — create, rename, share, invite collaborators, and archive. Archived projects move
            into an Archived section so they do not clutter the main list, are tagged archived
            automatically, and cannot accept new tasks until the owner restores them. Kanban projects can name sprints
            with optional descriptions, date ranges, and a lock date on the Sprints tab
            in project settings; ranges cannot overlap. After a sprint's lock date,
            only the project owner can add tasks. The board switcher filters tasks by
            sprint or backlog.
          </li>
          <li>
            Create and edit tags in project settings. Owners and editors can rename a tag and pick
            its color; chips on lists, boards, and the task sidebar use that color. Personal (inbox)
            tags are managed on your profile. System tags such as <strong>removed</strong> and
            <strong>archived</strong> cannot be edited.
          </li>
          <li>
            Save the current filter set as a view so you can reopen it later from the sidebar or
            <RouterLink to="/views">Views</RouterLink>.
          </li>
        </ul>

        <h2 id="calendar-dashboard" class="h4 mt-4">Calendar and dashboard</h2>
        <p>
          <RouterLink to="/calendar">Calendar</RouterLink>
          shows tasks by due date so you can plan the month at a glance.
          <RouterLink to="/dashboard">Dashboard</RouterLink>
          summarizes progress, overdue items, and other quick stats.
        </p>

        <h2 id="collaboration" class="h4 mt-4">Live collaboration</h2>
        <p>
          Shared projects stay in sync while you keep the page open. When someone else (or another of
          your tabs) creates, edits, moves, or deletes a task, the list, board, calendar, and
          dashboard refresh on their own. You do not need to reload the browser.
        </p>
        <ul>
          <li>
            If you are mid-edit in the task sidebar, Ordryn will not overwrite your draft. You will
            see a notice so you can save or discard first.
          </li>
          <li>
            Public share-link pages stay a snapshot until you refresh; live updates require a signed-in
            session.
          </li>
          <li>
            Owners, editors, and viewers can discuss a project task in the sidebar.
            You can edit your own comments; project owners can also edit anyone else’s.
            Each comment shows when it was posted and, if changed, when it was last edited.
            Hover the edited date to see who originally posted it (and who edited it, if that was someone else).
            Deleting a comment leaves a tombstone (“Message deleted by user” or “Message deleted by project owner”).
            Project owners and site admins can open History to review previous text and restore it.
            Site admins also have a Comment history log under Admin. New comments show up live for
            anyone with the task open. The notification bell refreshes when you open it, change pages,
            or return to the tab.
          </li>
          <li>
            Type <code>@</code> in a comment to mention a project member. Autocomplete lists matching
            members of that project only (same prefix search as project invites). Mentioned members
            get a notification; other members still see the usual new-comment notice.
          </li>
          <li>
            Paste <code>#123</code> in a comment to link another task you can access (including tasks
            only you can see). Click <strong>Insert link</strong>, and it renders as
            <em>Task #123 - Title</em> for anyone who can open that task.
          </li>
          <li>
            Use <strong>Insert image</strong>, or paste/drop a file, in a comment (when image hosting
            is enabled). After you post, the picture appears inline in that comment.
          </li>
        </ul>

        <h2 id="shortcuts" class="h4 mt-4">Keyboard shortcuts</h2>
        <p>
          Shortcuts work on the home page task list. Press <kbd>?</kbd> anytime to open the shortcuts
          help modal, or use the <strong>Shortcuts</strong> link in the header.
        </p>
        <table class="table table-sm">
          <thead>
            <tr>
              <th>Key</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td><kbd>n</kbd></td>
              <td>New task</td>
            </tr>
            <tr>
              <td><kbd>/</kbd></td>
              <td>Focus search</td>
            </tr>
            <tr>
              <td><kbd>Esc</kbd></td>
              <td>Close sidebar or modal</td>
            </tr>
            <tr>
              <td><kbd>j</kbd> / <kbd>k</kbd></td>
              <td>Move focus between tasks</td>
            </tr>
            <tr>
              <td><kbd>Enter</kbd> / <kbd>e</kbd></td>
              <td>Edit focused task</td>
            </tr>
            <tr>
              <td><kbd>d</kbd></td>
              <td>Delete focused task</td>
            </tr>
            <tr>
              <td><kbd>x</kbd></td>
              <td>Toggle complete</td>
            </tr>
            <tr>
              <td><kbd>?</kbd></td>
              <td>Show shortcuts help</td>
            </tr>
          </tbody>
        </table>

        <h2 id="settings-api" class="h4 mt-4">Settings and API</h2>
        <p>
          Update your profile under
          <RouterLink to="/settings">Settings</RouterLink>,
          grouped into Account, Preferences, Integrations, Data, and Developer.
          On Account you can optionally enable two-factor authentication with an authenticator app.
          After you confirm a code, you get five recovery codes — store them somewhere safe.
          Turning MFA off requires a current authenticator code or a remaining recovery code.
          Use <strong>Set up</strong> or <strong>Manage</strong> to open the two-factor dialog.
          Password reset does not disable MFA; the next login still asks for a code.
          For machine clients and integrations, see the
          <RouterLink to="/docs/api/v1">REST API documentation</RouterLink>.
          Site admins configure outbound email and image hosting (S3-compatible or local uploads) on
          <RouterLink to="/admin">Admin</RouterLink>.
          After filling in image hosting, use <strong>Test connection</strong> to confirm the server
          can upload to the bucket or local directory.
        </p>
      </div>
    </div>
  </div>
</template>
