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

export function updateFilterChips() {
  const chipsEl = document.getElementById("filter-active-chips");
  if (!chipsEl) return;

  chipsEl.innerHTML = "";
  let count = 0;

  Object.keys(FILTER_LABELS).forEach((key) => {
    const val = getFilterValue(key);
    if (!val) return;
    count += 1;
    const chip = document.createElement("span");
    chip.className = "filter-chip";
    chip.textContent = `${FILTER_LABELS[key]}: ${val}`;
    chipsEl.appendChild(chip);
  });

  chipsEl.classList.toggle("has-chips", count > 0);

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
