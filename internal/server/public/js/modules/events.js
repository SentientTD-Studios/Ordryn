import { apiPath } from "./utils.js";
import {
  initializeSidebarEventListeners,
  closeSidebar,
  attachEditButtonListeners,
  attachContextualCloseSidebar,
  handleSidebarAwareSettle,
  openSidebar,
} from "./sidebar.js";
import { initializeModalEventListeners } from "./modal.js";
import {
  initializeProjectFormHandlers,
  initCharacterCounters,
} from "./form-handlers.js";
import { showToast } from "./notifications.js";
import { initSortable } from "./sortable.js";

/** Registry of hooks run after manual task-list HTML swaps. */
const postSwapHooks = [];

export function registerPostSwapHook(fn) {
  postSwapHooks.push(fn);
}

function runPostSwapHooks(container) {
  for (const fn of postSwapHooks) {
    try {
      fn(container);
    } catch (e) {}
  }
}

/** Replace task list HTML from fetch responses and re-bind HTMX + UI hooks. */
export function swapTaskContainerHtml(html) {
  const container = document.getElementById("task-container");
  if (!container) return false;

  container.innerHTML = html;
  try {
    if (typeof htmx !== "undefined") htmx.process(container);
  } catch (e) {}
  try {
    initSortable();
    syncSortButtonState();
    syncFilterToolbarState();
    initializeModalEventListeners();
    attachEditButtonListeners();
    runPostSwapHooks(container);
  } catch (e) {}
  document.body.dispatchEvent(new CustomEvent("bulk-list-updated"));
  return true;
}

function getTaskListPage() {
  const pageInput = document.getElementById("current-page");
  if (pageInput && pageInput.value) {
    const page = parseInt(pageInput.value, 10);
    if (!Number.isNaN(page) && page > 0) {
      return page;
    }
  }
  return 1;
}

function buildTaskListUrl(page, options = {}) {
  let url = apiPath(`/api/fetch-tasks?page=${page}`);
  const searchInput = document.getElementById("search");
  if (searchInput && searchInput.value) {
    url += `&search=${encodeURIComponent(searchInput.value)}`;
  }
  const appendHidden = (id, param, override) => {
    if (override !== undefined) {
      if (override !== "") {
        url += `&${param}=${encodeURIComponent(override)}`;
      }
      return;
    }
    const el = document.getElementById(id);
    if (el && el.value) {
      url += `&${param}=${encodeURIComponent(el.value)}`;
    }
  };
  appendHidden("due-filter", "due", options.due);
  appendHidden("completed-filter", "completed", options.completed);
  appendHidden("sort-filter", "sort", options.sort);
  const readFilter = (toolbarId, hiddenId, param, override) => {
    if (override !== undefined) {
      if (override !== "") url += `&${param}=${encodeURIComponent(override)}`;
      return;
    }
    const toolbar = document.getElementById(toolbarId);
    if (toolbar && toolbar.value) {
      url += `&${param}=${encodeURIComponent(toolbar.value)}`;
      return;
    }
    appendHidden(hiddenId, param);
  };
  readFilter("status-filter-select", "status-filter", "status", options.status);
  readFilter("tag-filter-toolbar", "tag-filter", "tag", options.tag);
  readFilter("priority-filter-toolbar", "priority-filter", "priority", options.priority);
  const projectFilter = document.getElementById("project-filter-value");
  if (projectFilter && projectFilter.value) {
    url += `&project=${encodeURIComponent(projectFilter.value)}`;
  } else {
    const projectSelect = document.getElementById("project-filter");
    if (projectSelect && projectSelect.value) {
      url += `&project=${encodeURIComponent(projectSelect.value)}`;
    }
  }
  return url;
}

export function syncDueFilterButtons() {
  const dueEl = document.getElementById("due-filter");
  if (!dueEl) return;
  const activeDue = dueEl.value || "";
  document.querySelectorAll(".due-filter-btn").forEach((btn) => {
    const btnDue = btn.getAttribute("data-due") ?? "";
    const isActive = btnDue === activeDue;
    btn.classList.toggle("due-filter-active", isActive);
    btn.setAttribute("aria-pressed", isActive ? "true" : "false");
  });
}

export function syncFilterToolbarState() {
  const sync = (hiddenId, toolbarId) => {
    const hidden = document.getElementById(hiddenId);
    const toolbar = document.getElementById(toolbarId);
    if (hidden && toolbar && toolbar.value !== hidden.value) {
      toolbar.value = hidden.value;
    }
  };
  sync("status-filter", "status-filter-select");
  sync("tag-filter", "tag-filter-toolbar");
  sync("priority-filter", "priority-filter-toolbar");
  syncDueFilterButtons();
}

export function syncSortButtonState() {
  const btn = document.getElementById("sort-priority-btn");
  const sortEl = document.getElementById("sort-filter");
  if (!btn || !sortEl) return;

  const isPriority = sortEl.value === "priority";
  btn.textContent = isPriority ? "Sort: Priority" : "Sort: Manual";
  btn.classList.toggle("btn-primary", isPriority);
  btn.classList.toggle("btn-outline-primary", !isPriority);
  btn.setAttribute("aria-pressed", isPriority ? "true" : "false");
  btn.title = isPriority
    ? "Sorted by priority (high first). Click to restore manual drag order."
    : "Sorted by manual order. Click to sort by priority (high first).";
}

export function attachSortToggleListener() {
  document.body.addEventListener("click", function (evt) {
    const btn = evt.target.closest("#sort-priority-btn");
    if (!btn) return;

    const sortEl = document.getElementById("sort-filter");
    const current = sortEl ? sortEl.value : "";
    const nextSort = current === "priority" ? "" : "priority";

    htmx.ajax("GET", buildTaskListUrl(1, { sort: nextSort }), {
      target: "#task-container",
      swap: "innerHTML",
    });
  });
}

export function attachTaskDeletedListener() {
  document.body.addEventListener("taskDeleted", function () {
    htmx.ajax("GET", buildTaskListUrl(getTaskListPage()), {
      target: "#task-container",
      swap: "innerHTML",
    });
  });
}

export function attachReloadPageListener() {
  document.body.addEventListener("reloadPage", function (evt) {
    const page = evt.detail.page || getTaskListPage();
    htmx.ajax("GET", buildTaskListUrl(page), {
      target: "#task-container",
      swap: "innerHTML",
    });
  });
}

export function attachReloadPreviousPageListener() {
  document.body.addEventListener("reload-previous-page", function () {
    const prevPage = Math.max(getTaskListPage() - 1, 1);
    htmx.ajax("GET", buildTaskListUrl(prevPage), {
      target: "#task-container",
      swap: "innerHTML",
    });
  });
}

export function attachLoginSuccessListener() {
  document.body.addEventListener("login-success", function () {
    const loginModal = document.getElementById("loginmodal");
    if (loginModal) {
      const modal = bootstrap.Modal.getInstance(loginModal);
      if (modal) {
        modal.hide();
      }
    }

    window.location.reload();
  });
}

export function attachTaskCountsChangedListener() {
  document.body.addEventListener("taskCountsChanged", function (evt) {
    try {
      const d = evt.detail || {};
      if (typeof d.completed !== "undefined") {
        const el = document.getElementById("completed-tasks-badge");
        if (el) el.textContent = `Completed: ${d.completed}`;
      }
      if (typeof d.incomplete !== "undefined") {
        const el2 = document.getElementById("incomplete-tasks-badge");
        if (el2) el2.textContent = `Incomplete: ${d.incomplete}`;
      }
      // update total if both present
      if (
        typeof d.completed !== "undefined" &&
        typeof d.incomplete !== "undefined"
      ) {
        const totalEl = document.getElementById("total-tasks-badge");
        if (totalEl)
          totalEl.textContent = `Total Tasks: ${d.completed + d.incomplete}`;
      }
    } catch (e) {
      // ignore
    }
  });
}

export function attachHTMXAfterRequestListener() {
  document.body.addEventListener("htmx:afterRequest", function (evt) {
    try {
      const xhr = evt && evt.detail && evt.detail.xhr;
      if (!xhr || !xhr.responseURL) return;

      // Load edit form only — not edit-task saves (URL also contains "/api/edit")
      if (
        xhr.responseURL.includes("/api/edit") &&
        !xhr.responseURL.includes("/api/edit-task")
      ) {
        // Only open on success (2xx)
        const status = xhr.status || 0;
        if (status >= 200 && status < 300) {
          try {
            initializeSidebarEventListeners();
          } catch (e) {}
          openSidebar();
        }
      }

      // Clear create-project form after successful HTMX create
      if (xhr.responseURL.includes("/api/projects/create")) {
        const status = xhr.status || 0;
        // Check if this is a validation error
        const isValidationError =
          xhr.getResponseHeader &&
          xhr.getResponseHeader("X-Validation-Error") === "true";

        if (status >= 200 && status < 300 && !isValidationError) {
          try {
            const form = document.getElementById("createProjectForm");
            if (form) {
              const nameInput = form.querySelector('input[name="name"]');
              if (nameInput) nameInput.value = "";
              const charCount = document.getElementById(
                "project-name-char-count",
              );
              if (charCount) charCount.textContent = "0";
              const errorDiv = document.getElementById("project-name-error");
              if (errorDiv) errorDiv.innerHTML = "";
            }
            // Reinitialize project form handlers
            initializeProjectFormHandlers();
            if (typeof showToast === "function") showToast("Project created.");
          } catch (e) {}
        }
      }

      // When server notifies projects changed, refresh project selects
      try {
        if (
          xhr &&
          xhr.getResponseHeader &&
          xhr.getResponseHeader("HX-Trigger")
        ) {
          const trig = xhr.getResponseHeader("HX-Trigger");
          if (trig && trig.indexOf("projects-changed") !== -1) {
            fetch(apiPath("/api/projects/json"))
              .then((res) => res.json())
              .then((data) => {
                try {
                  // Update all selects with id project_id
                  const selects =
                    document.querySelectorAll("select#project_id");
                  selects.forEach((sel) => {
                    // preserve current value
                    const cur = sel.value;
                    // clear existing options
                    while (sel.options.length > 1) sel.remove(1);
                    data.forEach((p) => {
                      const opt = document.createElement("option");
                      opt.value = p.id;
                      opt.textContent = p.name;
                      sel.appendChild(opt);
                    });
                    // restore value if still present
                    try {
                      sel.value = cur;
                    } catch (e) {}
                  });
                } catch (e) {}
              })
              .catch(() => {});
          }

          // If server requested setting the toolbar project filter, apply it
          if (trig && trig.indexOf("set-project-filter") !== -1) {
            try {
              const m = trig.match(/set-project-filter:([^\s]+)/);
              if (m && m[1] !== undefined) {
                const val = m[1];
                const pf = document.querySelector("select#project-filter");
                if (pf) {
                  pf.value = val;
                  // Do not dispatch change here — server already returned the correct view
                }
              }
            } catch (e) {}
          }
        }
      } catch (e) {}
    } catch (e) {}
  });
}

export function attachHTMXAfterSettleListener() {
  document.body.addEventListener("htmx:afterSettle", (event) => {
    if (event.target.id === "task-container") {
      attachEditButtonListeners();
    }
  });
}

export function attachHTMXAfterSwapListeners() {
  // Handle sidebar-aware HTMX swaps
  handleSidebarAwareSettle();

  // If HTMX swapped the sidebar, ensure it's opened and listeners attached
  document.body.addEventListener("htmx:afterSwap", function (evt) {
    try {
      const target =
        evt.detail && evt.detail.target ? evt.detail.target : evt.target;
      if (target && target.id === "sidebar") {
        try {
          initializeSidebarEventListeners();
        } catch (e) {}
        openSidebar();
      }
    } catch (e) {}
  });

  // Also handle cases where we replace only the sidebar body via innerHTML
  document.body.addEventListener("htmx:afterSwap", function (evt) {
    try {
      const detail = evt && evt.detail;
      const swapped = detail && detail.target ? detail.target : evt.target;
      if (swapped && swapped.id === "sidebar") return; // handled above
      // when swapping innerHTML into '#sidebar .sidebar-body', the event target will be that element
      if (
        swapped &&
        swapped.classList &&
        swapped.classList.contains("sidebar-body")
      ) {
        try {
          initializeSidebarEventListeners();
        } catch (e) {}
        openSidebar();
      }
    } catch (e) {}
  });

  // Reattach modal listeners after HTMX swaps that replace task container
  document.body.addEventListener("htmx:afterSwap", function (evt) {
    if (evt.target && evt.target.id === "task-container") {
      try {
        syncSortButtonState();
        syncFilterToolbarState();
      } catch (e) {}
      try {
        initializeModalEventListeners();
      } catch (e) {}
    }
  });
}

export function attachHTMXErrorListeners() {
  document.body.addEventListener("htmx:responseError", (evt) => {
    const xhr = evt.detail?.xhr;
    if (!xhr) return;
    if (xhr.getResponseHeader?.("X-Validation-Error") === "true") return;
    const url = xhr.responseURL || "";
    if (url.includes("/api/validate-description")) return;

    const status = xhr.status;
    const msg =
      status && status >= 500
        ? "Something went wrong on the server. Please try again."
        : "Request failed. Please try again.";
    showToast(msg, { error: true });
  });

  document.body.addEventListener("htmx:sendError", () => {
    showToast("Network error. Check your connection and try again.", {
      error: true,
    });
  });
}

export function attachHTMXLoadingListeners() {
  let savedScrollY = 0;
  const loadingTargets = new Set([
    "task-container",
    "import-result",
    "sidebar",
  ]);

  function isLoadingTarget(evt) {
    const detail = evt.detail;
    if (!detail) return false;
    const targetSel =
      detail.elt?.getAttribute?.("hx-target") ||
      detail.target?.getAttribute?.("id");
    if (targetSel === "#task-container" || targetSel === "task-container") {
      return true;
    }
    if (targetSel === "#import-result" || targetSel === "import-result") {
      return true;
    }
    if (
      targetSel === "#sidebar .sidebar-body" ||
      detail.target?.classList?.contains("sidebar-body")
    ) {
      return true;
    }
    const targetId = detail.target?.id;
    return targetId && loadingTargets.has(targetId);
  }

  document.body.addEventListener("htmx:beforeRequest", (evt) => {
    if (!isLoadingTarget(evt)) return;
    document.body.classList.add("htmx-loading");
    const container = document.getElementById("task-container");
    if (
      container &&
      (evt.detail.target?.id === "task-container" ||
        evt.detail.elt?.getAttribute("hx-target") === "#task-container")
    ) {
      savedScrollY = window.scrollY;
      container.classList.add("task-container-loading");
    }
  });

  document.body.addEventListener("htmx:afterRequest", () => {
    document.body.classList.remove("htmx-loading");
  });

  document.body.addEventListener("htmx:afterSettle", (evt) => {
    const target = evt.detail?.target || evt.target;
    if (!target) return;

    if (target.id === "task-container") {
      target.classList.remove("task-container-loading");
      target.classList.add("task-container-fade-in");
      window.scrollTo(0, savedScrollY);
      setTimeout(() => target.classList.remove("task-container-fade-in"), 300);
    }
  });
}

export function attachAllEventListeners() {
  document.body.addEventListener("click", (e) => {
    const dueBtn = e.target.closest(".due-filter-btn");
    if (dueBtn) {
      const activeDue = dueBtn.getAttribute("data-due") ?? "";
      document.querySelectorAll(".due-filter-btn").forEach((btn) => {
        const btnDue = btn.getAttribute("data-due") ?? "";
        const isActive = btnDue === activeDue;
        btn.classList.toggle("due-filter-active", isActive);
        btn.setAttribute("aria-pressed", isActive ? "true" : "false");
      });
    }
  });

  attachTaskDeletedListener();
  attachReloadPageListener();
  attachReloadPreviousPageListener();
  attachLoginSuccessListener();
  attachTaskCountsChangedListener();
  attachSortToggleListener();
  attachHTMXAfterRequestListener();
  attachHTMXAfterSettleListener();
  attachHTMXAfterSwapListeners();
  attachHTMXLoadingListeners();
  attachHTMXErrorListeners();
  attachEditButtonListeners();
  attachContextualCloseSidebar();
  syncSortButtonState();
  syncFilterToolbarState();
}
