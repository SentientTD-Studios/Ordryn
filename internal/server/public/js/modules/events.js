import { apiPath } from "./utils.js";
import {
  initializeSidebarEventListeners,
  closeSidebar,
  attachEditButtonListeners,
  attachContextualCloseSidebar,
  handleSidebarAwareSettle,
} from "./sidebar.js";
import { initializeModalEventListeners } from "./modal.js";
import {
  initializeProjectFormHandlers,
  initCharacterCounters,
} from "./form-handlers.js";
import { showToast } from "./notifications.js";

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

      if (xhr.responseURL.includes("/api/edit")) {
        // Only open on success (2xx)
        const status = xhr.status || 0;
        if (status >= 200 && status < 300) {
          try {
            initializeSidebarEventListeners();
          } catch (e) {}
          const sb = document.getElementById("sidebar");
          if (sb) sb.classList.add("active");
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
        const sb = document.getElementById("sidebar");
        if (sb) sb.classList.add("active");
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
        const sb = document.getElementById("sidebar");
        if (sb) sb.classList.add("active");
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
  attachEditButtonListeners();
  attachContextualCloseSidebar();
  syncSortButtonState();
  syncFilterToolbarState();
}
