import { apiPath } from "./utils.js";
import { syncDueFilterButtons, syncFilterToolbarState, syncSortButtonState } from "./events.js";

const FILTER_LABELS = {
  project: "Project",
  status: "Status",
  tag: "Tag",
  due: "Due",
  priority: "Priority",
  sort: "Sort",
};

const DUE_LABELS = {
  "": "All",
  today: "Today",
  overdue: "Overdue",
  week: "This week",
  none: "No date",
};

const STATUS_LABELS = {
  "": "All",
  incomplete: "Incomplete",
  complete: "Complete",
};

const PRIORITY_LABELS = {
  "": "All",
  "1": "Low",
  "2": "Medium",
  "3": "High",
};

function setHiddenValue(id, value) {
  const el = document.getElementById(id);
  if (el) el.value = value;
}

function getFilterValue(name) {
  switch (name) {
    case "project": {
      const el = document.getElementById("project-filter");
      if (!el || !el.value) return null;
      if (el.value === "0") return "No project";
      return el.options[el.selectedIndex]?.text || el.value;
    }
    case "status": {
      const el = document.getElementById("status-filter-select");
      if (!el || !el.value) return null;
      return STATUS_LABELS[el.value] || el.value;
    }
    case "tag": {
      const el = document.getElementById("tag-filter-toolbar");
      if (!el || !el.value) return null;
      return el.options[el.selectedIndex]?.text || el.value;
    }
    case "due": {
      const el = document.getElementById("due-filter");
      if (!el || !el.value) return null;
      return DUE_LABELS[el.value] || el.value;
    }
    case "priority": {
      const el = document.getElementById("priority-filter-toolbar");
      if (!el || !el.value) return null;
      return PRIORITY_LABELS[el.value] || el.value;
    }
    case "sort": {
      const el = document.getElementById("sort-filter");
      if (!el || el.value !== "priority") return null;
      return "Priority";
    }
    default:
      return null;
  }
}

export function clearFilter(key) {
  switch (key) {
    case "project": {
      const el = document.getElementById("project-filter");
      if (el) el.value = "";
      setHiddenValue("project-filter-value", "");
      break;
    }
    case "status": {
      const el = document.getElementById("status-filter-select");
      if (el) el.value = "";
      setHiddenValue("status-filter", "");
      break;
    }
    case "tag": {
      const el = document.getElementById("tag-filter-toolbar");
      if (el) el.value = "";
      setHiddenValue("tag-filter", "");
      break;
    }
    case "due":
      setHiddenValue("due-filter", "");
      document.querySelectorAll(".due-filter-btn").forEach((btn) => {
        const isAll = (btn.getAttribute("data-due") ?? "") === "";
        btn.classList.toggle("due-filter-active", isAll);
        btn.setAttribute("aria-pressed", isAll ? "true" : "false");
      });
      break;
    case "priority": {
      const el = document.getElementById("priority-filter-toolbar");
      if (el) el.value = "";
      setHiddenValue("priority-filter", "");
      break;
    }
    case "sort":
      setHiddenValue("sort-filter", "");
      syncSortButtonState();
      break;
    default:
      break;
  }
}

export function clearAllFilters() {
  const search = document.getElementById("search");
  if (search) search.value = "";
  Object.keys(FILTER_LABELS).forEach(clearFilter);
  syncFilterToolbarState();
  syncDueFilterButtons();
  htmx.ajax("GET", apiPath("/api/fetch-tasks?page=1"), {
    target: "#task-container",
    swap: "innerHTML",
  });
}

export function updateFilterChips() {
  const chipsEl = document.getElementById("filter-active-chips");
  if (!chipsEl) return;

  chipsEl.innerHTML = "";
  let count = 0;

  Object.keys(FILTER_LABELS).forEach((key) => {
    const val = getFilterValue(key);
    if (!val) return;
    count += 1;
    const chip = document.createElement("button");
    chip.type = "button";
    chip.className = "filter-chip";
    chip.dataset.filterKey = key;
    chip.setAttribute("aria-label", `Remove ${FILTER_LABELS[key]} filter: ${val}`);
    chip.innerHTML = `${FILTER_LABELS[key]}: ${val} <i class="bi bi-x ms-1" aria-hidden="true"></i>`;
    chipsEl.appendChild(chip);
  });

  chipsEl.classList.toggle("has-chips", count > 0);

  const clearAllBtn = document.getElementById("filter-clear-all");
  if (clearAllBtn) {
    clearAllBtn.classList.toggle("d-none", count === 0);
  }

  const toggleBtn = document.getElementById("filter-toggle-btn");
  if (toggleBtn) {
    toggleBtn.setAttribute(
      "aria-expanded",
      document.getElementById("filter-toolbar-panel")?.classList.contains("collapsed")
        ? "false"
        : "true",
    );
  }
}

export function initFilterToolbar() {
  const toggleBtn = document.getElementById("filter-toggle-btn");
  const panel = document.getElementById("filter-toolbar-panel");
  if (!toggleBtn || !panel) return;

  const mq = window.matchMedia("(max-width: 991px)");

  function applyCollapsedState() {
    if (mq.matches) {
      panel.classList.add("collapsed");
      toggleBtn.setAttribute("aria-expanded", "false");
    } else {
      panel.classList.remove("collapsed");
      toggleBtn.setAttribute("aria-expanded", "true");
    }
    updateFilterChips();
  }

  toggleBtn.addEventListener("click", () => {
    panel.classList.toggle("collapsed");
    toggleBtn.setAttribute(
      "aria-expanded",
      panel.classList.contains("collapsed") ? "false" : "true",
    );
  });

  const clearAllBtn = document.getElementById("filter-clear-all");
  if (clearAllBtn) {
    clearAllBtn.addEventListener("click", (e) => {
      e.preventDefault();
      clearAllFilters();
    });
  }

  document.body.addEventListener("click", (e) => {
    if (e.target.closest("#empty-clear-filters") || e.target.closest("#filter-clear-all")) {
      e.preventDefault();
      clearAllFilters();
      return;
    }
    if (e.target.closest("#empty-clear-search")) {
      e.preventDefault();
      const search = document.getElementById("search");
      if (search) search.value = "";
      htmx.ajax("GET", apiPath("/api/fetch-tasks?page=1"), {
        target: "#task-container",
        swap: "innerHTML",
      });
      return;
    }
    if (e.target.closest("#empty-add-task")) {
      e.preventDefault();
      const openBtn = document.getElementById("openSidebar");
      if (openBtn) openBtn.click();
    }
  });

  document.body.addEventListener("click", (e) => {
    const chip = e.target.closest(".filter-chip[data-filter-key]");
    if (!chip) return;
    e.preventDefault();
    clearFilter(chip.dataset.filterKey);
    syncFilterToolbarState();
    htmx.ajax("GET", apiPath("/api/fetch-tasks?page=1"), {
      target: "#task-container",
      swap: "innerHTML",
    });
  });

  mq.addEventListener("change", applyCollapsedState);
  applyCollapsedState();

  document.body.addEventListener("htmx:afterSwap", (evt) => {
    if (evt.target && evt.target.id === "task-container") {
      updateFilterChips();
    }
  });

  ["project-filter", "status-filter-select", "tag-filter-toolbar", "priority-filter-toolbar"].forEach(
    (id) => {
      const el = document.getElementById(id);
      if (el) el.addEventListener("change", updateFilterChips);
    },
  );

  document.body.addEventListener("click", (e) => {
    if (e.target.closest(".due-filter-btn")) {
      setTimeout(updateFilterChips, 0);
    }
  });

  document.body.addEventListener("click", (e) => {
    if (e.target.closest("#sort-priority-btn")) {
      setTimeout(updateFilterChips, 100);
    }
  });
}
