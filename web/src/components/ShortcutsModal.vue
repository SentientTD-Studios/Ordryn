<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>

<template>
  <div
    id="shortcutsModal"
    class="modal fade"
    tabindex="-1"
    aria-labelledby="shortcutsModalLabel"
    aria-hidden="true"
  >
    <div class="modal-dialog modal-dialog-centered">
      <div class="modal-content">
        <div class="modal-header">
          <h5 id="shortcutsModalLabel" class="modal-title">Keyboard Shortcuts</h5>
          <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close" />
        </div>
        <div class="modal-body">
          <p class="text-muted small">
            Shortcuts apply on the home page task list. Press <kbd>Esc</kbd> to leave search or close
            the sidebar if keys seem ignored.
          </p>
          <p class="text-muted small">
            After deleting tasks, an undo toast appears for a short time when available.
          </p>
          <table class="table table-sm mb-3">
            <tbody>
              <tr>
                <td><kbd>Ctrl</kbd>/<kbd>⌘</kbd>+<kbd>K</kbd></td>
                <td>Search tasks and projects, jump to <code>#123</code>, or run a command (works on every page)</td>
              </tr>
              <tr>
                <td><kbd>n</kbd></td>
                <td>New task (open sidebar)</td>
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
                <td>Move between tasks</td>
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
                <td>Toggle complete on focused task</td>
              </tr>
              <tr>
                <td><kbd>?</kbd> or <kbd>Shift</kbd>+<kbd>/</kbd></td>
                <td>Show this help</td>
              </tr>
            </tbody>
          </table>
          <p class="mb-0 small">
            <RouterLink to="/docs/guide" data-bs-dismiss="modal">Full guide →</RouterLink>
          </p>
        </div>
      </div>
    </div>
  </div>
</template>
