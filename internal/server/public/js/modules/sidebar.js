import { apiPath } from "./utils.js";
import { attachThemeToggle, initTheme } from "./theme.js";
import { bindDueDatePresets, handleDescriptionInput } from "./form-handlers.js";

let sidebarFocusHandler = null;
let lastFocusedBeforeSidebar = null;

function getSidebarFocusable(sidebar) {
  return Array.from(
    sidebar.querySelectorAll(
      'button:not([disabled]), [href], input:not([type="hidden"]):not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  ).filter((el) => el.offsetParent !== null || el === document.activeElement);
}

function syncSidebarFilterFields(form) {
  const setHidden = (name, value) => {
    let field = form.querySelector(`input[name="${name}"]`);
    if (!field) {
      field = document.createElement("input");
      field.type = "hidden";
      field.name = name;
      form.appendChild(field);
    }
    field.value = value || "";
  };
  const projectSelect = document.querySelector("select#project-filter");
  const statusSelect = document.getElementById("status-filter-select");
  const statusHidden = document.getElementById("status-filter");
  const dueHidden = document.getElementById("due-filter");
  const sortHidden = document.getElementById("sort-filter");
  const priorityHidden = document.getElementById("priority-filter");
  const priorityToolbar = document.getElementById("priority-filter-toolbar");
  const tagHidden = document.getElementById("tag-filter");
  const tagToolbar = document.getElementById("tag-filter-toolbar");

  setHidden("project", projectSelect ? projectSelect.value : "");
  setHidden(
    "status",
    statusSelect
      ? statusSelect.value
      : statusHidden
        ? statusHidden.value
        : "",
  );
  setHidden("due", dueHidden ? dueHidden.value : "");
  setHidden("sort", sortHidden ? sortHidden.value : "");
  setHidden(
    "priority",
    priorityToolbar
      ? priorityToolbar.value
      : priorityHidden
        ? priorityHidden.value
        : "",
  );
  setHidden(
    "tag",
    tagToolbar ? tagToolbar.value : tagHidden ? tagHidden.value : "",
  );
}

const SIDEBAR_LOADING_HTML =
  '<div class="sidebar-loading" aria-busy="true">' +
  '<div class="spinner-border text-primary" role="status">' +
  '<span class="visually-hidden">Loading task…</span></div>' +
  '<p class="mb-0">Loading task…</p></div>';

function getSidebarBackdrop() {
  return document.getElementById("sidebar-backdrop");
}

function setSidebarBackdrop(active) {
  const backdrop = getSidebarBackdrop();
  if (!backdrop) return;
  backdrop.classList.toggle("active", active);
  backdrop.setAttribute("aria-hidden", active ? "false" : "true");
}

function showSidebarLoading() {
  const sidebar = document.getElementById("sidebar");
  if (!sidebar) return;
  const body = sidebar.querySelector(".sidebar-body");
  if (body) body.innerHTML = SIDEBAR_LOADING_HTML;
  const title = sidebar.querySelector(".sidebar-header h5");
  if (title) title.textContent = "Edit Task";
}

export function openSidebar() {
  const sidebar = document.getElementById("sidebar");
  if (!sidebar) return;

  lastFocusedBeforeSidebar = document.activeElement;
  sidebar.classList.add("active");
  setSidebarBackdrop(true);

  const focusables = getSidebarFocusable(sidebar);
  const first = focusables[0] || sidebar.querySelector("#title");
  if (first) {
    first.focus();
  }

  if (sidebarFocusHandler) {
    document.removeEventListener("keydown", sidebarFocusHandler);
  }
  sidebarFocusHandler = (e) => {
    if (e.key !== "Tab" || !sidebar.classList.contains("active")) return;
    const items = getSidebarFocusable(sidebar);
    if (items.length === 0) return;
    const firstEl = items[0];
    const lastEl = items[items.length - 1];
    if (e.shiftKey && document.activeElement === firstEl) {
      e.preventDefault();
      lastEl.focus();
    } else if (!e.shiftKey && document.activeElement === lastEl) {
      e.preventDefault();
      firstEl.focus();
    }
  };
  document.addEventListener("keydown", sidebarFocusHandler);
}

export function closeSidebar() {
  const sidebar = document.getElementById("sidebar");
  if (sidebar) {
    sidebar.classList.remove("active");
  }
  setSidebarBackdrop(false);
  if (sidebarFocusHandler) {
    document.removeEventListener("keydown", sidebarFocusHandler);
    sidebarFocusHandler = null;
  }
  if (
    lastFocusedBeforeSidebar &&
    typeof lastFocusedBeforeSidebar.focus === "function"
  ) {
    try {
      lastFocusedBeforeSidebar.focus();
    } catch (e) {}
  }
}

export function initializeSidebarEventListeners() {
  // Re-query buttons in case HTMX replaced the DOM inside #task-container
  const openBtn = document.getElementById("openSidebar");
  const closeBtn = document.getElementById("closeSidebar");

  if (openBtn) {
    openBtn.removeEventListener("click", openSidebar); // Prevent duplicate bindings
    openBtn.addEventListener("click", function (ev) {
      try {
        const tf = document.getElementById("newTaskForm");
        if (tf) {
          const titleEl = tf.querySelector("#title");
          if (titleEl) titleEl.value = "";
          const descEl = tf.querySelector("#description");
          if (descEl) descEl.value = "";
          const projEl = tf.querySelector("#project_id");
          if (projEl) projEl.value = "";
          const dueEl = tf.querySelector("#due_date");
          if (dueEl) dueEl.value = "";
          const idInput = tf.querySelector('input[name="id"]');
          if (idInput) idInput.remove();
          const submit = tf.querySelector('button[type="submit"]');
          if (submit) submit.textContent = "Add Task";
          try {
            tf.setAttribute("hx-post", apiPath("/api/add-task"));
          } catch (e) {}
          const cp = tf.querySelector('input[name="currentPage"]');
          if (cp) {
            const pageEl = document.getElementById("current-page");
            cp.value = (pageEl && pageEl.value) || "1";
          }
          const newTagsEl = tf.querySelector("#new_tags");
          if (newTagsEl) newTagsEl.value = "";
          tf.querySelectorAll('input[name="tag_ids"]').forEach((cb) => {
            cb.checked = false;
          });
          syncSidebarFilterFields(tf);
          const sbTitle = document.querySelector("#sidebar .sidebar-header h5");
          if (sbTitle) sbTitle.textContent = "Add Task";
          const charCount = document.getElementById("char-count");
          if (charCount) charCount.textContent = "0";
        }
      } catch (e) {}
      openSidebar();
    });
  }

  if (closeBtn) {
    closeBtn.removeEventListener("click", closeSidebar); // Prevent duplicate bindings
    closeBtn.addEventListener("click", closeSidebar);
  }

  // Reattach theme toggle if needed
  attachThemeToggle();

  try {
    const tf = document.getElementById("newTaskForm");
    if (tf) {
      bindDueDatePresets(tf);
    }
  } catch (e) {}

  // Reattach task form submit listener so dynamically swapped forms behave the same
  try {
    const tf = document.getElementById("newTaskForm");
    if (tf && !tf.classList.contains("task-form-initialized")) {
      // Ensure hidden project field exists and is kept up-to-date before submit
      try {
        const tf = document.getElementById("newTaskForm");
        if (tf) {
          syncSidebarFilterFields(tf);
          tf.addEventListener("submit", function () {
            syncSidebarFilterFields(tf);
          });
        }
      } catch (e) {}
      tf.addEventListener("htmx:afterRequest", (event) => {
        const elt = event.detail && event.detail.elt;
        if (elt !== tf) return;

        const xhr = event.detail && event.detail.xhr;
        const responseURL = xhr && xhr.responseURL ? xhr.responseURL : "";
        const isAddTask = responseURL.includes("/api/add-task");
        const isEditTask = responseURL.includes("/api/edit-task");
        if (!isAddTask && !isEditTask) return;

        let isValidationError = false;
        try {
          const header =
            xhr && xhr.getResponseHeader
              ? xhr.getResponseHeader("X-Validation-Error")
              : null;
          if (header && header.toLowerCase() === "true") {
            isValidationError = true;
          } else if (
            event.detail &&
            event.detail.triggerSpec &&
            event.detail.triggerSpec.trigger === "description-error"
          ) {
            isValidationError = true;
          }
        } catch (e) {}

        if (event.detail.successful && !isValidationError) {
          closeSidebar();
          // Only reset fields after adding a task — not after editing
          if (isAddTask) {
            const tEl = tf.querySelector("#title");
            if (tEl) tEl.value = "";
            const dEl = tf.querySelector("#description");
            if (dEl) dEl.value = "";
            const charCount = tf.querySelector("#char-count");
            if (charCount) charCount.textContent = "0";
            const errorDiv = tf.querySelector("#description-error");
            if (errorDiv) errorDiv.innerHTML = "";
          }
        }
      });
      tf.classList.add("task-form-initialized");
    }
  } catch (e) {}
}

export function attachEditButtonListeners() {
  // When an edit button is clicked, open the sidebar immediately so the user sees the form loading
  document.body.removeEventListener("click", handleEditButtonClick);
  document.body.addEventListener("click", handleEditButtonClick);
}

function handleEditButtonClick(e) {
  try {
    const btn = e.target && e.target.closest && e.target.closest(".edit-btn");
    if (!btn) return;
    showSidebarLoading();
    openSidebar();
  } catch (e) {}
}

export function attachContextualCloseSidebar() {
  // Delegated close button handler: works even if the sidebar markup was swapped
  document.body.removeEventListener("click", handleSidebarCloseClick);
  document.body.addEventListener("click", handleSidebarCloseClick);

  const backdrop = getSidebarBackdrop();
  if (backdrop) {
    backdrop.removeEventListener("click", closeSidebar);
    backdrop.addEventListener("click", closeSidebar);
  }
}

function handleSidebarCloseClick(e) {
  try {
    const close =
      e.target && e.target.closest && e.target.closest("#closeSidebar");
    if (!close) return;
    closeSidebar();
  } catch (e) {}
}

export function handleSidebarAwareSettle() {
  // Re-initialize character counter and theme toggle after HTMX swaps if sidebar is active
  document.body.removeEventListener(
    "htmx:afterSwap",
    handleAfterSwapForSidebar,
  );
  document.body.addEventListener("htmx:afterSwap", handleAfterSwapForSidebar);
}

function handleAfterSwapForSidebar(event) {
  const sidebarElement = document.getElementById("sidebar");
  if (!sidebarElement || !sidebarElement.classList.contains("active")) return;

  const target = (event.detail && event.detail.target) || event.target;
  if (!target) return;

  // Ignore inline validation / activity loads — only react to full sidebar swaps.
  if (target.id === "description-error") return;
  if (target.closest && target.closest("details.task-timeline")) return;
  if (
    !target.classList.contains("sidebar-body") &&
    target.id !== "sidebar"
  ) {
    return;
  }

  let description = document.getElementById("description");
  let charCount = document.getElementById("char-count");
  if (description && charCount) {
    handleDescriptionInput(charCount);
  }
  if (typeof initTheme === "function") {
    initTheme();
  }
  const tf = document.getElementById("newTaskForm");
  if (tf) {
    bindDueDatePresets(tf);
    syncSidebarFilterFields(tf);
    const first = sidebarElement.querySelector(
      '#title, input:not([type="hidden"])',
    );
    if (first) first.focus();
  }
}
