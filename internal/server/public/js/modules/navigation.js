import { initTheme, attachThemeToggle } from "./theme.js";
import { initHomePage } from "./home-init.js";
import { initProfilePage } from "./profile.js";
import { initCalendarPage } from "./calendar.js";
import { initDashboardCharts } from "./dashboard.js";
import { initAnnouncementCharCounter } from "./admin.js";
import {
  initCharacterCounters,
  initializeProjectFormHandlers,
  initializeProjectRenameHandlers,
  initializeTagRenameHandlers,
} from "./form-handlers.js";
import {
  initializeSidebarEventListeners,
  attachEditButtonListeners,
} from "./sidebar.js";
import { initializeModalEventListeners } from "./modal.js";
import { initSortable } from "./sortable.js";
import { syncFilterToolbarState, syncSortButtonState } from "./events.js";
import { updateFilterChips, syncFiltersToURL } from "./filters.js";
import { initSavedViews } from "./saved-views.js";

function focusMainAfterBoost(main) {
  if (!main) return;
  if (!main.hasAttribute("tabindex")) {
    main.setAttribute("tabindex", "-1");
  }
  requestAnimationFrame(() => {
    main.focus({ preventScroll: true });
  });
}

function processHtmxInElement(root) {
  if (typeof htmx === "undefined" || !root) return;
  htmx.process(root);
}

function reinitHomePage(main) {
  initHomePage(main);
  processHtmxInElement(main);
  try {
    initSortable();
    syncSortButtonState();
    syncFilterToolbarState();
    updateFilterChips();
    syncFiltersToURL();
    initSavedViews();
    initializeSidebarEventListeners();
    initializeModalEventListeners();
    attachEditButtonListeners();
  } catch (e) {}
}

function reinitProjectsPage() {
  initCharacterCounters();
  initializeProjectFormHandlers();
  initializeProjectRenameHandlers();
  initializeTagRenameHandlers();
}

function reinitPageByType(pageType, main) {
  switch (pageType) {
    case "home":
      reinitHomePage(main);
      break;
    case "profile":
      initProfilePage();
      break;
    case "calendar":
      initCalendarPage();
      break;
    case "dashboard":
      initDashboardCharts();
      break;
    case "projects":
      reinitProjectsPage();
      break;
    case "admin":
      initAnnouncementCharCounter();
      break;
    default:
      break;
  }
}

/** Re-init page modules after hx-boost navigation. */
export function initNavigation() {
  if (typeof htmx === "undefined") return;
  document.body.addEventListener("htmx:afterSwap", (e) => {
    if (!e.detail?.boosted) return;
    const main = document.querySelector("main");
    if (!main) return;

    initTheme();
    attachThemeToggle();

    const pageType = main.dataset.page || "";
    reinitPageByType(pageType, main);
    focusMainAfterBoost(main);
  });
}
