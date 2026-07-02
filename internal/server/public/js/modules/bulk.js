import { apiPath } from "./utils.js";
import { initSortable } from "./sortable.js";
import { syncSortButtonState, syncFilterToolbarState } from "./events.js";

export function initBulkActions() {
  const selected = new Set();
  let lastClickedIndex = -1;

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
    const btn = e.target.closest("[data-bulk-action]");
    if (!btn) return;
    e.preventDefault();

    const action = btn.dataset.bulkAction;
    if (!action || selected.size === 0) return;

    if (action === "delete") {
      const count = selected.size;
      if (
        !window.confirm(
          `Delete ${count} task${count === 1 ? "" : "s"}? This cannot be undone.`,
        )
      ) {
        return;
      }
    }

    const form = new URLSearchParams();
    form.append("action", action);
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
        const container = document.getElementById("task-container");
        if (container) {
          container.innerHTML = html;
          document.body.dispatchEvent(new CustomEvent("bulk-list-updated"));
        }
        clearSelection();
        try {
          initSortable();
          syncSortButtonState();
          syncFilterToolbarState();
        } catch (e) {}
        if (typeof window.showToast === "function") {
          window.showToast("Bulk action completed.");
        }
      })
      .catch((err) => {
        if (typeof window.showToast === "function") {
          window.showToast(err.message || "Bulk action failed.");
        }
      });
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

function getCurrentPage() {
  const pageInput = document.getElementById("current-page");
  if (pageInput && pageInput.value) {
    const page = parseInt(pageInput.value, 10);
    if (!Number.isNaN(page) && page > 0) return String(page);
  }
  return "1";
}

function appendFilterFields(form) {
  const append = (name, hiddenId, toolbarId) => {
    let val = "";
    if (toolbarId) {
      const toolbar = document.getElementById(toolbarId);
      if (toolbar) val = toolbar.value;
    }
    if (!val && hiddenId) {
      const hidden = document.getElementById(hiddenId);
      if (hidden) val = hidden.value;
    }
    if (val) form.append(name, val);
  };
  append("project", "project-filter-value", "project-filter");
  append("status", "status-filter", "status-filter-select");
  append("due", "due-filter", null);
  append("sort", "sort-filter", null);
  append("priority", "priority-filter", "priority-filter-toolbar");
  append("tag", "tag-filter", "tag-filter-toolbar");
  const search = document.getElementById("search");
  if (search && search.value) form.append("search", search.value);
}
