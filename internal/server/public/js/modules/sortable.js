import { apiPath } from "./utils.js";
import { swapTaskContainerHtml } from "./events.js";
import { showToast } from "./notifications.js";

function sortableOptions(isFav) {
  const coarse = window.matchMedia("(pointer: coarse)").matches;
  return {
    handle: ".drag-handle",
    draggable: "tr.task-row",
    animation: 150,
    delay: coarse ? 200 : 0,
    delayOnTouchOnly: true,
    touchStartThreshold: coarse ? 5 : 1,
    onEnd: function (evt) {
      const ids = Array.from(evt.to.querySelectorAll("tr.task-row"))
        .map((row) => {
          const id = row.id || "";
          return id.replace("task-", "");
        })
        .filter(Boolean)
        .join(",");

      const form = new URLSearchParams();
      form.append("order", ids);
      form.append("is_favorite", isFav ? "true" : "false");
      const pageEl = document.querySelector(
        '#task-container [name="currentPage"]',
      );
      if (pageEl && pageEl.value) form.append("page", pageEl.value);
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

      fetch(apiPath("/api/reorder-tasks"), {
        method: "POST",
        headers: {
          "HX-Request": "true",
          "X-Requested-With": "XMLHttpRequest",
        },
        body: form,
        redirect: "follow",
      })
        .then((resp) => {
          if (resp.redirected) {
            window.location.href = resp.url;
            return null;
          }

          const hxRedirect =
            resp.headers.get("HX-Redirect") || resp.headers.get("Hx-Redirect");
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
          const lower = html.slice(0, 600).toLowerCase();
          if (
            lower.includes("<html") ||
            lower.includes("<body") ||
            lower.includes("<nav") ||
            lower.includes("gotodo")
          ) {
            window.location.reload();
            return;
          }
          swapTaskContainerHtml(html);
        })
        .catch((err) => {
          console.error("Reorder failed", err);
          showToast("Could not save task order. Please try again.", {
            error: true,
          });
        });
    },
  };
}

export function initSortable() {
  try {
    if (typeof Sortable === "undefined") return;

    const favList = document.getElementById("favorite-task-list");
    const regList = document.getElementById("task-list");

    const createSortable = (el, isFav) => {
      if (!el) return;
      if (el._sortable) {
        try {
          el._sortable.destroy();
        } catch (e) {}
      }
      el._sortable = Sortable.create(el, sortableOptions(isFav));
    };

    createSortable(favList, true);
    createSortable(regList, false);
  } catch (e) {
    // ignore
  }
}

export function attachSortableInitializers() {
  initSortable();
  document.body.addEventListener("htmx:afterSwap", function (evt) {
    const target = (evt.detail && evt.detail.target) || evt.target;
    if (!target) return;

    const isTaskContainer = target.id === "task-container";
    const isTaskRow =
      target.tagName === "TR" &&
      target.id &&
      target.id.startsWith("task-");

    if (!isTaskContainer && !isTaskRow) return;
    initSortable();
  });
}
