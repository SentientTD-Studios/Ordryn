import { apiPath } from "./utils.js";
import { swapTaskContainerHtml } from "./events.js";
import { showToast } from "./notifications.js";
import { appendFilterFields } from "./filter-fields.js";
import { formatDateInput } from "./form-handlers.js";

export function initBulkActions() {
  const selected = new Set();
  let lastClickedIndex = -1;
  let bulkInFlight = false;

  function getVisibleCheckboxes() {
    return Array.from(document.querySelectorAll("#task-container .task-select"));
  }

  function updateBulkBar() {
    const bar = document.getElementById("bulk-bar");
    const countEl = document.getElementById("bulk-count");
    if (!bar || !countEl) return;
    const count = selected.size;
    countEl.textContent = `${count} selected`;
    bar.classList.toggle("d-none", count === 0);
    bar.setAttribute("aria-hidden", count === 0 ? "true" : "false");
  }

  function setBulkLoading(loading) {
    bulkInFlight = loading;
    document.querySelectorAll("#bulk-bar [data-bulk-action]").forEach((btn) => {
      btn.disabled = loading;
    });
    const clearBtn = document.getElementById("bulk-clear-selection");
    if (clearBtn) clearBtn.disabled = loading;
  }

  function syncSelectAllState() {
    const selectAll = document.getElementById("select-all-tasks");
    if (!selectAll) return;
    const boxes = getVisibleCheckboxes();
    if (boxes.length === 0) {
      selectAll.checked = false;
      selectAll.indeterminate = false;
      return;
    }
    const checkedCount = boxes.filter((cb) => cb.checked).length;
    selectAll.checked = checkedCount === boxes.length;
    selectAll.indeterminate = checkedCount > 0 && checkedCount < boxes.length;
  }

  function clearSelection() {
    selected.clear();
    getVisibleCheckboxes().forEach((cb) => {
      cb.checked = false;
    });
    lastClickedIndex = -1;
    updateBulkBar();
    syncSelectAllState();
  }

  function setCheckboxChecked(cb, checked) {
    const id = parseInt(cb.dataset.taskId, 10);
    if (Number.isNaN(id)) return;
    cb.checked = checked;
    if (checked) {
      selected.add(id);
    } else {
      selected.delete(id);
    }
  }

  document.body.addEventListener("change", (e) => {
    const selectAll = e.target.closest("#select-all-tasks");
    if (selectAll) {
      const checked = selectAll.checked;
      getVisibleCheckboxes().forEach((cb) => setCheckboxChecked(cb, checked));
      updateBulkBar();
      syncSelectAllState();
      return;
    }

    const cb = e.target.closest(".task-select");
    if (!cb) return;

    const boxes = getVisibleCheckboxes();
    const index = boxes.indexOf(cb);

    if (e.shiftKey && lastClickedIndex >= 0 && index >= 0) {
      const start = Math.min(lastClickedIndex, index);
      const end = Math.max(lastClickedIndex, index);
      const checked = cb.checked;
      for (let i = start; i <= end; i++) {
        setCheckboxChecked(boxes[i], checked);
      }
    } else {
      setCheckboxChecked(cb, cb.checked);
      lastClickedIndex = index;
    }

    updateBulkBar();
    syncSelectAllState();
  });

  document.body.addEventListener("click", (e) => {
    const presetBtn = e.target.closest("[data-bulk-due-preset]");
    if (presetBtn) {
      e.preventDefault();
      const input = document.getElementById("bulk-due-date");
      if (!input) return;
      const preset = presetBtn.dataset.bulkDuePreset;
      if (preset === "clear") {
        input.value = "";
        return;
      }
      const date = new Date();
      if (preset === "tomorrow") date.setDate(date.getDate() + 1);
      else if (preset === "week") date.setDate(date.getDate() + 7);
      input.value = formatDateInput(date);
      return;
    }

    const clearBtn = e.target.closest("#bulk-clear-selection");
    if (clearBtn) {
      e.preventDefault();
      clearSelection();
      return;
    }

    const btn = e.target.closest("[data-bulk-action]");
    if (!btn) return;
    e.preventDefault();

    const action = btn.dataset.bulkAction;
    if (!action || selected.size === 0 || bulkInFlight) return;

    if (action === "delete") {
      const count = selected.size;
      confirmBulkDelete(count).then((ok) => {
        if (!ok) return;
        runBulkAction(action, selected, clearSelection, setBulkLoading);
      });
      return;
    }

    runBulkAction(action, selected, clearSelection, setBulkLoading);
  });

  document.body.addEventListener("bulk-list-updated", () => {
    syncSelectAllState();
  });

  document.body.addEventListener("htmx:afterSwap", (evt) => {
    if (evt.target && evt.target.id === "task-container") {
      clearSelection();
    }
  });
}

function confirmBulkDelete(count) {
  return new Promise((resolve) => {
    const modalEl = document.getElementById("modal");
    const content = modalEl?.querySelector(".modal-content");
    if (!modalEl || !content || typeof bootstrap === "undefined") {
      resolve(false);
      return;
    }

    const taskLabel =
      count === 1 ? "this task" : `these ${count} tasks`;
    content.innerHTML = `
      <div class="modal-header">
        <h5 class="modal-title">Confirm Delete</h5>
        <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
      </div>
      <div class="modal-body">
        <p>Are you sure you want to delete ${taskLabel}? You can undo for up to 120 seconds afterward.</p>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Cancel</button>
        <button type="button" class="btn btn-danger" id="bulk-delete-confirm">Yes, Delete</button>
      </div>
    `;

    const modal = bootstrap.Modal.getOrCreateInstance(modalEl);
    const confirmBtn = document.getElementById("bulk-delete-confirm");
    let settled = false;

    const finish = (ok) => {
      if (settled) return;
      settled = true;
      confirmBtn?.removeEventListener("click", onConfirm);
      modalEl.removeEventListener("hidden.bs.modal", onHidden);
      resolve(ok);
    };

    const onConfirm = () => {
      finish(true);
      modal.hide();
    };

    const onHidden = () => {
      finish(false);
    };

    confirmBtn?.addEventListener("click", onConfirm);
    modalEl.addEventListener("hidden.bs.modal", onHidden);
    modal.show();
  });
}

function runBulkAction(action, selected, clearSelection, setBulkLoading) {
  const form = new URLSearchParams();
  let serverAction = action;
  if (action === "clear_due_date") {
    serverAction = "set_due_date";
  }
  form.append("action", serverAction);
  form.append("ids", Array.from(selected).join(","));
  form.append("page", getCurrentPage());

  appendFilterFields(form);

  if (action === "move_project") {
    const sel = document.getElementById("bulk-project");
    if (!sel || !sel.value) return;
    form.append("project_id", sel.value);
  }
  if (action === "add_tag" || action === "remove_tag") {
    const sel = document.getElementById("bulk-tag");
    if (!sel || !sel.value) return;
    form.append("tag_id", sel.value);
  }
  if (action === "set_priority") {
    const sel = document.getElementById("bulk-priority");
    if (!sel) return;
    form.append("priority", sel.value);
  }
  if (action === "set_due_date") {
    const input = document.getElementById("bulk-due-date");
    if (!input || !input.value) return;
    form.append("due_date", input.value);
  }
  if (action === "clear_due_date") {
    form.append("due_date", "");
  }

  setBulkLoading(true);
  const deleteCount = action === "delete" ? selected.size : 0;

  fetch(apiPath("/api/bulk-update"), {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      "HX-Request": "true",
    },
    body: form.toString(),
  })
    .then((res) => {
      if (!res.ok) {
        return res.text().then((t) => {
          throw new Error(t || "Bulk action failed");
        });
      }
      return res.text();
    })
    .then((html) => {
      swapTaskContainerHtml(html);
      clearSelection();
      if (action === "delete") {
        document.body.dispatchEvent(
          new CustomEvent("task-deleted", { detail: { count: deleteCount } }),
        );
      } else {
        showToast("Bulk action completed.");
      }
    })
    .catch((err) => {
      showToast(err.message || "Bulk action failed.", { error: true });
    })
    .finally(() => {
      setBulkLoading(false);
    });
}

function getCurrentPage() {
  const pageInput = document.getElementById("current-page");
  if (pageInput && pageInput.value) {
    const page = parseInt(pageInput.value, 10);
    if (!Number.isNaN(page) && page > 0) return String(page);
  }
  return "1";
}
