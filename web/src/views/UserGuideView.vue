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
            <li class="list-inline-item"><a href="#roles-permissions">Roles</a></li>
            <li class="list-inline-item"><a href="#ai-agents">AI agents</a></li>
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
          save. Saving a task also posts any comment you have typed but not posted yet. If you try to
          close the sidebar with unsaved field changes or unposted comment text, Ordryn asks whether to
          <strong>Save</strong>, <strong>Discard</strong>, or <strong>Stay</strong>. When an admin has enabled image hosting, paste or drop a JPEG, PNG, GIF, or WebP
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
            — create, rename, share, invite collaborators, and archive. Group people in
            <RouterLink to="/organizations">Organizations</RouterLink>
            and import those members when you create a project, or attach an organization later
            from project settings. Inviting someone to an organization works like a project invite:
            they must accept before they join. After they accept they are added to attached projects
            that imported everyone (copy or lock). Boards where members were chosen manually are
            skipped. Use Sync members on the organization page to catch up existing copy and lock
            projects. When you attach an organization you choose how to
            import: copy everyone and keep roles editable, copy everyone and lock roles to the
            organization, or pick specific members and a role for each. People who are not imported
            are removed from the project. Organization role changes update imported-and-locked
            projects only. Archived projects move
            into an Archived section so they do not clutter the main list, are tagged archived
            automatically, and cannot accept new tasks until the owner restores them. Kanban projects can name sprints
            with optional descriptions, date ranges, and a lock date on the Sprints tab
            in project settings; ranges cannot overlap. After a sprint's lock date,
            only the project owner can add tasks. The board switcher filters tasks by
            sprint or backlog.
          </li>
          <li>
            Create and edit tags in project settings. Members with the manage-tags permission
            (owners, editors, and custom roles that include it) can rename a tag and pick
            its color; chips on lists, boards, and the task sidebar use that color. Personal (inbox)
            tags are managed on your profile. System tags such as <strong>removed</strong> and
            <strong>archived</strong> cannot be edited.
          </li>
          <li>
            Save the current filter set as a view so you can reopen it later from the sidebar or
            <RouterLink to="/views">Views</RouterLink>.
          </li>
        </ul>

        <h2 id="roles-permissions" class="h4 mt-4">Roles and status gates</h2>
        <p>
          Project membership uses site-wide roles (Owner, Editor, Viewer, plus extras such as
          Developer and QA) with a catalog of permissions: create, edit, delete, archive, restore,
          complete, claim, reorder, change status, assign sprints, manage tags, log time, configure
          extensions, moderate comments, and manage the project. Site admins maintain those templates
          under Admin → Roles. Copy a role to start from an existing permission set, and drag to
          reorder the list. Organizations can customize the name and permissions of those site
          defaults for their own members (except Owner, which always has every permission) without
          changing the site-wide templates. Reset a customized role to restore the site default;
          members keep the same role slug. Organizations can also define extra roles for imported
          projects. If you import and lock roles, those boards stay tied to the organization and
          cannot add their own roles. Unlocked imports can still add project-only roles.
        </p>
        <ul>
          <li>
            Assign roles from project settings → Sharing. Discussion posts show the author’s project
            role next to their name (for example <em>Ryan - Owner</em> or <em>Dev - Developer II</em>).
          </li>
          <li>
            QA typically cannot create or delete tasks, but can claim cards and move them across
            statuses while testing.
          </li>
          <li>
            On a kanban board, each status can restrict which roles may move tasks in or out.
            Empty lists mean any role with “change status” may move that direction. Owners and
            site admins always bypass gates. A common setup is Ready for QA (open) and In QA
            (QA plus owner only).
          </li>
        </ul>

        <h2 id="ai-agents" class="h4 mt-4">AI agents</h2>
        <p>
          Project managers can add AI agents to a project and hand them tasks. An agent is a project
          member with its own <code>@handle</code>, a role, and guardrails, so its comments, claims, and
          edits show up under its name (with an <strong>AI agent</strong> badge) rather than yours. The
          agent itself runs outside {{ siteName }}: for example Claude Code on a developer machine, a CI
          job, or your own service. {{ siteName }} tells it when there is work, hands it the task, and
          records what it did.
        </p>
        <p>
          <strong>What you need:</strong> an agent set up on the project, and something connected to its key
          that runs a model. A GitHub repository is <em>not</em> required. It only matters for agents that change
          code.
        </p>
        <p>
          <strong>What the agent sees:</strong> the task's title, description, status, priority, due date, tags,
          custom fields, and discussion, plus the note sent with the run and your standing instructions. If the project
          has a linked GitHub repository or the task has a linked issue, those are included too. That's all it knows.
          A one-line task like “Fix login” gives it little to work with. Say what you want back and how you'll
          judge it done, in the task or in the note.
        </p>
        <p><strong>What it's good for</strong>, depending on where the agent runs:</p>
        <table class="table table-sm small">
          <thead><tr><th>Starting setup</th><th>Send it…</th><th>You get back</th><th>Runs on</th></tr></thead>
          <tbody>
            <tr><td>Triage &amp; clarify</td><td>Rough or one-line requests</td><td>A rewritten description with steps and “done when” criteria, a priority, and open questions; card moved to Review</td><td>Any model, including local</td></tr>
            <tr><td>Break down into subtasks</td><td>A feature or epic</td><td>3–10 subtasks plus a plan comment</td><td>Any model</td></tr>
            <tr><td>Draft &amp; write</td><td>“Write the release notes for…”, “Draft an email to…”</td><td>The draft as a comment, with assumptions and questions</td><td>Any model</td></tr>
            <tr><td>Investigate code (read-only)</td><td>A bug report or “how does this work?”</td><td>Findings with file:line references and a proposed patch to apply yourself</td><td>An agent that can read the code, e.g. the local Ollama example with <code>REPO_DIR</code></td></tr>
            <tr><td>Code changes</td><td>A bug or small feature, ideally with a linked issue</td><td>A branch or pull request, tests, and a summary comment</td><td>An agent with the code checked out, e.g. Claude Code over MCP</td></tr>
          </tbody>
        </table>
        <ol>
          <li>
            Open project settings → <strong>AI agents</strong> and choose <strong>Add AI agent</strong>.
            Pick what it should do (this fills in instructions and guardrails), a name, a handle, and a project
            role. Roles that can manage the project are not allowed.
          </li>
          <li>
            Write <strong>standing instructions</strong>: how to do the work, what “done” means, and when
            to stop and ask in a comment instead.
          </li>
          <li>
            Choose <strong>triggers</strong>: an <code>@mention</code> in a comment (the comment becomes the
            run's note), moving a card into a column such as “AI queue”, or <strong>Send to agent</strong>
            on the task. Decide who may send work: managers only (the default), anyone who can edit tasks, or only the
            roles and specific members you pick. Calls from anyone else are ignored and nothing happens.
          </li>
          <li>
            Set <strong>guardrails</strong>: which fields it may change, which columns it may move cards
            into, and whether it may complete tasks, create tasks, or comment. By default it may only move
            cards and comment, cannot complete anything, and is limited to 20 runs per hour. Keep
            “complete tasks” off and limit it to a Review column if you want a person to sign off.
          </li>
          <li>
            Under <strong>Connect the agent</strong>, create a key. For Claude Code or another MCP client,
            copy the <code>claude mcp add …</code> command shown there. For your own service, set a webhook
            URL so each new run is POSTed to it, or poll the agent's queue over the API.
          </li>
        </ol>
        <p>
          On a task, the <strong>AI agents</strong> section shows each run (queued, running, succeeded,
          failed, or cancelled), the agent's summary, and a <strong>Send to agent</strong> button with an
          optional note for extra direction or acceptance criteria. While a run is open the card is claimed
          by the agent. The claim is released when it finishes. Managers, or whoever started a run, can
          cancel it.
        </p>
        <ul>
          <li>
            Agents never trigger other agents. They cannot sign in, receive no notifications, and are
            managed only from the AI agents tab, not Sharing.
          </li>
          <li>
            <strong>Pause</strong> stops new runs and refuses the agent's keys right away.
            <strong>Remove</strong> revokes its keys, cancels open runs, and takes it off the project.
            Its past comments and activity stay, and its handle stays reserved.
          </li>
          <li>
            Anything members write on a task becomes input the agent reads. Keep triggers limited to
            people you trust, and give the agent only the role and guardrails it needs.
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
            You do not have to hit Post before Save: saving the task posts any comment still in the box.
            Closing the sidebar with unposted comment text (or unsaved task edits) asks Save / Discard / Stay.
            You can edit your own comments; members with moderate-comments (project owners by default)
            can also edit anyone else’s.
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
          <RouterLink to="/docs/api/v2">REST API documentation</RouterLink>.
          Site admins configure outbound email for password resets and invites only (not extensions;
          core sends are rate-limited) and image hosting (S3-compatible or local uploads) on
          <RouterLink to="/admin">Admin</RouterLink>.
          After filling in image hosting, use <strong>Test connection</strong> to confirm the server
          can upload to the bucket or local directory.
        </p>
      </div>
    </div>
  </div>
</template>
