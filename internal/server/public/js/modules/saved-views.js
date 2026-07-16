import { apiPath } from "./utils.js";
import {
  syncDueFilterButtons,
  syncFilterToolbarState,
  syncSortButtonState,
} from "./events.js";
import { updateFilterChips, syncFiltersToURL } from "./filters.js";
import { showToast } from "./notifications.js";

let savedViewsBound = false;
const SAVED_VIEW_NAME_MAX_LENGTH = 80;

function getCurrentFilterState() {
  const val = (id) => {
    const el = document.getElementById(id);
    return el ? el.value : "";
  };
  return {
    project: val("project-filter") || val("project-filter-value"),
    status: val("status-filter-select") || val("status-filter"),
    due: val("due-filter"),
    completed: val("completed-filter"),
    priority: val("priority-filter-toolbar") || val("priority-filter"),
    tag: val("tag-filter-toolbar") || val("tag-filter"),
    sort: val("sort-filter"),
    search: (document.getElementById("search") || {}).value || "",
  };
}

function setHiddenValue(id, value) {
  const el = document.getElementById(id);
  if (el) el.value = value ?? "";
}

export function applyFilterState(filter) {
  if (!filter) return;

  const projectEl = document.getElementById("project-filter");
  if (projectEl) projectEl.value = filter.project || "";
  setHiddenValue("project-filter-value", filter.project || "");

  const statusEl = document.getElementById("status-filter-select");
  if (statusEl) statusEl.value = filter.status || "";
  setHiddenValue("status-filter", filter.status || "");

  const tagEl = document.getElementById("tag-filter-toolbar");
  if (tagEl) tagEl.value = filter.tag || "";
  setHiddenValue("tag-filter", filter.tag || "");

  const priorityEl = document.getElementById("priority-filter-toolbar");
  if (priorityEl) priorityEl.value = filter.priority || "";
  setHiddenValue("priority-filter", filter.priority || "");

  setHiddenValue("due-filter", filter.due || "");
  setHiddenValue("completed-filter", filter.completed || "");
  setHiddenValue("sort-filter", filter.sort || "");

  const search = document.getElementById("search");
  if (search) search.value = filter.search || "";

  document.querySelectorAll(".due-filter-btn").forEach((btn) => {
    const due = btn.getAttribute("data-due") ?? "";
    const active = due === (filter.due || "");
    btn.classList.toggle("due-filter-active", active);
    btn.setAttribute("aria-pressed", active ? "true" : "false");
  });

  syncFilterToolbarState();
  syncSortButtonState();
  updateFilterChips();

  htmx.ajax("GET", buildFetchTasksUrl(filter), {
    target: "#task-container",
    swap: "innerHTML",
  });
}

function buildFetchTasksUrl(filter) {
  const params = new URLSearchParams({ page: "1" });
  Object.entries(filter || {}).forEach(([key, value]) => {
    if (value) params.set(key, value);
  });
  return apiPath(`/api/fetch-tasks?${params.toString()}`);
}

async function fetchSavedViews() {
  const res = await fetch(apiPath("/api/saved-views/json"), {
    credentials: "same-origin",
  });
  if (!res.ok) return [];
  return res.json();
}

function renderSavedViewsMenu(views) {
  const menu = document.getElementById("saved-views-menu");
  if (!menu) return;
  menu.innerHTML = "";

  const saveLi = document.createElement("li");
  saveLi.innerHTML =
    '<button type="button" class="dropdown-item" id="saved-view-save-btn"><i class="bi bi-plus-lg me-1"></i> Save current view…</button>';
  menu.appendChild(saveLi);

  if (!views || views.length === 0) {
    const empty = document.createElement("li");
    empty.innerHTML =
      '<span class="dropdown-item-text text-muted small">No saved views yet</span>';
    menu.appendChild(empty);
    return;
  }

  const divider = document.createElement("li");
  divider.innerHTML = '<hr class="dropdown-divider" />';
  menu.appendChild(divider);

  views.forEach((view) => {
    const li = document.createElement("li");
    li.className = "dropdown-item-text d-flex align-items-center justify-content-between gap-2 px-3 py-1";
    li.innerHTML = `
      <button type="button" class="btn btn-link btn-sm p-0 text-start saved-view-apply" data-view-id="${view.id}">${escapeHtml(view.name)}</button>
      <span class="btn-group btn-group-sm">
        <button type="button" class="btn btn-outline-secondary btn-sm saved-view-rename" data-view-id="${view.id}" data-view-name="${escapeAttr(view.name)}" title="Rename"><i class="bi bi-pencil"></i></button>
        <button type="button" class="btn btn-outline-danger btn-sm saved-view-delete" data-view-id="${view.id}" title="Delete"><i class="bi bi-trash"></i></button>
      </span>`;
    li.querySelector(".saved-view-apply").dataset.filter = JSON.stringify(view.filter);
    menu.appendChild(li);
  });
}

function escapeHtml(s) {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}

function escapeAttr(s) {
  return String(s).replace(/"/g, "&quot;");
}

async function reloadSavedViews() {
  try {
    const views = await fetchSavedViews();
    renderSavedViewsMenu(views);
  } catch (e) {
    /* ignore */
  }
}

async function saveCurrentView(name, id, renameOnly) {
  const filter = getCurrentFilterState();
  const body = new URLSearchParams();
  body.set("name", name);
  if (id) body.set("id", String(id));
  if (renameOnly) body.set("rename_only", "true");
  Object.entries(filter).forEach(([k, v]) => {
    if (v) body.set(k, v);
  });

  const res = await fetch(apiPath("/api/saved-views/save"), {
    method: "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      "HX-Request": "true",
    },
    body: body.toString(),
  });
  const contentType = res.headers.get("Content-Type") || "";
  const data = contentType.includes("application/json")
    ? await res.json().catch(() => ({}))
    : {};
  if (!res.ok) {
    showToast(data.error || "Failed to save view", { error: true });
    return false;
  }
  if (!contentType.includes("application/json")) {
    showToast("Failed to save view", { error: true });
    return false;
  }
  showToast(renameOnly ? "View renamed" : "View saved");
  await reloadSavedViews();
  return true;
}

async function deleteView(id) {
  const body = new URLSearchParams({ id: String(id) });
  const res = await fetch(apiPath("/api/saved-views/delete"), {
    method: "POST",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      "HX-Request": "true",
    },
    body: body.toString(),
  });
  const contentType = res.headers.get("Content-Type") || "";
  if (!res.ok || !contentType.includes("application/json")) {
    showToast("Failed to delete view", { error: true });
    return;
  }
  showToast("View deleted");
  await reloadSavedViews();
}

function getSharedModal() {
  const modalEl = document.getElementById("modal");
  const content = modalEl?.querySelector(".modal-content");
  if (!modalEl || !content || typeof bootstrap === "undefined") {
    return null;
  }
  return { modalEl, content, modal: bootstrap.Modal.getOrCreateInstance(modalEl) };
}

function promptViewName({ title, label, submitLabel, initialValue = "" }) {
  return new Promise((resolve) => {
    const shared = getSharedModal();
    if (!shared) {
      resolve(null);
      return;
    }

    shared.content.innerHTML = `
      <form id="saved-view-name-form">
        <div class="modal-header">
          <h5 class="modal-title">${escapeHtml(title)}</h5>
          <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
        </div>
        <div class="modal-body">
          <label for="saved-view-name-input" class="form-label">${escapeHtml(label)}</label>
          <input type="text" class="form-control" id="saved-view-name-input" name="name" maxlength="${SAVED_VIEW_NAME_MAX_LENGTH}" required />
          <div class="form-text">Use up to ${SAVED_VIEW_NAME_MAX_LENGTH} characters.</div>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Cancel</button>
          <button type="submit" class="btn btn-primary">${escapeHtml(submitLabel)}</button>
        </div>
      </form>
    `;

    const form = document.getElementById("saved-view-name-form");
    const input = document.getElementById("saved-view-name-input");
    if (input) input.value = initialValue;

    let settled = false;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      form?.removeEventListener("submit", onSubmit);
      shared.modalEl.removeEventListener("hidden.bs.modal", onHidden);
      resolve(value);
    };

    const onSubmit = (e) => {
      e.preventDefault();
      const name = input?.value.trim() || "";
      if (!name) {
        input?.focus();
        return;
      }
      finish(name);
      shared.modal.hide();
    };

    const onHidden = () => {
      finish(null);
    };

    form?.addEventListener("submit", onSubmit);
    shared.modalEl.addEventListener("hidden.bs.modal", onHidden);
    shared.modal.show();
  });
}

function confirmDeleteView() {
  return new Promise((resolve) => {
    const shared = getSharedModal();
    if (!shared) {
      resolve(false);
      return;
    }

    shared.content.innerHTML = `
      <div class="modal-header">
        <h5 class="modal-title">Delete saved view?</h5>
        <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
      </div>
      <div class="modal-body">
        <p class="mb-0">This saved view will be removed. Your tasks will not be changed.</p>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Cancel</button>
        <button type="button" class="btn btn-danger" id="saved-view-delete-confirm">Delete View</button>
      </div>
    `;

    const confirmBtn = document.getElementById("saved-view-delete-confirm");
    let settled = false;
    const finish = (ok) => {
      if (settled) return;
      settled = true;
      confirmBtn?.removeEventListener("click", onConfirm);
      shared.modalEl.removeEventListener("hidden.bs.modal", onHidden);
      resolve(ok);
    };

    const onConfirm = () => {
      finish(true);
      shared.modal.hide();
    };

    const onHidden = () => {
      finish(false);
    };

    confirmBtn?.addEventListener("click", onConfirm);
    shared.modalEl.addEventListener("hidden.bs.modal", onHidden);
    shared.modal.show();
  });
}

export function initSavedViews() {
  const dropdown = document.getElementById("saved-views-dropdown");
  if (!dropdown) return;

  reloadSavedViews();

  if (savedViewsBound) return;
  savedViewsBound = true;

  document.body.addEventListener("click", async (e) => {
    if (e.target.closest("#saved-view-save-btn")) {
      e.preventDefault();
      const name = await promptViewName({
        title: "Save Current View",
        label: "View name",
        submitLabel: "Save View",
      });
      if (!name) return;
      await saveCurrentView(name, null, false);
      return;
    }

    const applyBtn = e.target.closest(".saved-view-apply");
    if (applyBtn) {
      e.preventDefault();
      try {
        const filter = JSON.parse(applyBtn.dataset.filter || "{}");
        applyFilterState(filter);
      } catch (err) {
        showToast("Could not apply view", { error: true });
      }
      return;
    }

    const renameBtn = e.target.closest(".saved-view-rename");
    if (renameBtn) {
      e.preventDefault();
      const id = renameBtn.dataset.viewId;
      const current = renameBtn.dataset.viewName || "";
      const name = await promptViewName({
        title: "Rename Saved View",
        label: "View name",
        submitLabel: "Rename View",
        initialValue: current,
      });
      if (!name || name === current) return;
      await saveCurrentView(name, id, true);
      return;
    }

    const deleteBtn = e.target.closest(".saved-view-delete");
    if (deleteBtn) {
      e.preventDefault();
      const id = deleteBtn.dataset.viewId;
      if (!(await confirmDeleteView())) return;
      await deleteView(id);
    }
  });

  document.body.addEventListener("htmx:afterSwap", (evt) => {
    if (evt.target && evt.target.id === "task-container") {
      syncFiltersToURL();
    }
  });
}
