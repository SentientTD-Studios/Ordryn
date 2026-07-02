import {
  apiPath,
  restoreFooterIfMissing,
  ensureTableClasses,
  ensureTableStructure,
} from "./utils.js";
import { initializeSidebarEventListeners } from "./sidebar.js";
import { initializeModalEventListeners } from "./modal.js";

export function initSortable() {
  try {
    if (typeof Sortable === "undefined") return;

    const favList = document.getElementById("favorite-task-list");
    const regList = document.getElementById("task-list");

    const createSortable = (el, isFav) => {
      if (!el) return;
      // Destroy existing Sortable instance if present
      if (el._sortable) {
        try {
          el._sortable.destroy();
        } catch (e) {}
      }
      el._sortable = Sortable.create(el, {
        handle: ".drag-handle",
        animation: 150,
        onEnd: function (evt) {
          // Build order of ids
          const ids = Array.from(evt.to.children)
            .map((row) => {
              const id = row.id || "";
              return id.replace("task-", "");
            })
            .filter(Boolean)
            .join(",");

          // Post new order to server
          const form = new URLSearchParams();
          form.append("order", ids);
          form.append("is_favorite", isFav ? "true" : "false");
          // include current page if present
          const pageEl = document.querySelector(
            '#task-container [name="currentPage"]',
          );
          if (pageEl && pageEl.value) form.append("page", pageEl.value);
          // include current toolbar project filter so server can respect scoped reorder
          try {
            const toolbar = document.querySelector("select#project-filter");
            const toolbarVal = toolbar ? toolbar.value : "";
            if (typeof toolbarVal !== "undefined" && toolbarVal !== null) {
              form.append("project", toolbarVal);
            }
            const statusFilter = document.getElementById("status-filter");
            const statusVal = statusFilter ? statusFilter.value : "";
            if (statusVal) {
              form.append("status", statusVal);
            }
            const dueFilter = document.getElementById("due-filter");
            const dueVal = dueFilter ? dueFilter.value : "";
            if (dueVal) {
              form.append("due", dueVal);
            }
            const sortFilter = document.getElementById("sort-filter");
            const sortVal = sortFilter ? sortFilter.value : "";
            if (sortVal) {
              form.append("sort", sortVal);
            }
            const priorityFilter = document.getElementById("priority-filter");
            const priorityVal = priorityFilter ? priorityFilter.value : "";
            if (priorityVal) {
              form.append("priority", priorityVal);
            }
            const tagFilter = document.getElementById("tag-filter");
            const tagVal = tagFilter ? tagFilter.value : "";
            if (tagVal) {
              form.append("tag", tagVal);
            }
          } catch (e) {}

          // Send HX-Request and X-Requested-With so server middleware accepts this as an XHR/HTMX call
          fetch(apiPath("/api/reorder-tasks"), {
            method: "POST",
            headers: {
              "HX-Request": "true",
              "X-Requested-With": "XMLHttpRequest",
              // Keep content-type unset when sending URLSearchParams body; browser will set it.
            },
            body: form,
            redirect: "follow",
          })
            .then((resp) => {
              // If the request was redirected (server issued 3xx), don't inject the returned full page:
              if (resp.redirected) {
                // Top-level navigate to the final location
                window.location.href = resp.url;
                return null;
              }

              // Respect HX-Redirect if server used it for HTMX flows
              const hxRedirect = resp.headers.get("HX-Redirect") || resp.headers.get("Hx-Redirect");
              if (hxRedirect) {
                window.location.href = hxRedirect;
                return null;
              }

              if (!resp.ok) {
                throw new Error("Failed to save order: " + resp.status);
              }
              return resp.text();
            })
            .then((html) => {
              if (!html) return;
              // Defensive: if server returned a full document instead of the fragment, reload instead of injecting
              const lower = html.slice(0, 600).toLowerCase();
              if (lower.includes("<html") || lower.includes("<body") || lower.includes("<nav") || lower.includes("gotodo")) {
                // Avoid injecting an entire page into the task container — reload to get a clean page
                window.location.reload();
                return;
              }

              // Replace task container with returned HTML
              const container = document.getElementById("task-container");
              if (container) {
                container.innerHTML = html;
                // Let HTMX process any hx-* attributes in the newly inserted content
                try {
                  if (typeof htmx !== "undefined") htmx.process(container);
                } catch (e) {}
                // Reinitialize sortable after DOM update
                try {
                  initSortable();
                } catch (e) {}
                // Reattach sidebar and modal listeners which may have been lost
                try {
                  if (typeof initializeSidebarEventListeners === "function") {
                    initializeSidebarEventListeners();
                  }
                } catch (e) {}
                try {
                  if (typeof initializeModalEventListeners === "function") {
                    initializeModalEventListeners();
                  }
                } catch (e) {}
                // Ensure footer still exists after manual replacement
                try {
                  restoreFooterIfMissing();
                } catch (e) {}
              }
            })
            .catch((err) => {
              console.error("Reorder failed", err);
            });
        },
      });
    };

    createSortable(favList, true);
    createSortable(regList, false);
  } catch (e) {
    // ignore
  }
}

export function attachSortableInitializers() {
  // Initialize sortable on initial load and after HTMX swaps
  initSortable();
  document.body.addEventListener("htmx:afterSwap", function (evt) {
    if (evt.target.id === "task-container") {
      // Ensure table retains expected Bootstrap classes after HTMX replaces content
      try {
        ensureTableStructure();
        ensureTableClasses();
      } catch (e) {}

      initSortable();
    }
  });
}
