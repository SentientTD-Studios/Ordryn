// Import all modules
import { apiPath } from "./modules/utils.js";
import { initTheme, toggleTheme, attachThemeToggle } from "./modules/theme.js";
import {
  initCharacterCounters,
  initializeProjectFormHandlers,
  initializeProjectRenameHandlers,
  initializeTagRenameHandlers,
  initDueDatePresets,
  handleDescriptionInput,
} from "./modules/form-handlers.js";
import {
  openSidebar,
  closeSidebar,
  initializeSidebarEventListeners,
  attachEditButtonListeners,
  attachContextualCloseSidebar,
  handleSidebarAwareSettle,
} from "./modules/sidebar.js";
import {
  initializeModalEventListeners,
  renderChangelog,
  loadChangelog,
  attachChangelogListener,
} from "./modules/modal.js";
import {
  initSortable,
  attachSortableInitializers,
} from "./modules/sortable.js";
import {
  showToast,
  attachNotificationListeners,
} from "./modules/notifications.js";
import { attachAllEventListeners } from "./modules/events.js";
import { initKeyboardShortcuts } from "./modules/keyboard.js";
import {
  initGlobalAnnouncement,
  dismissGlobalAnnouncement,
} from "./modules/announcement.js";
import { initAnnouncementCharCounter } from "./modules/admin.js";
import { initDescriptionToggles } from "./modules/descriptions.js";
import { initBulkActions } from "./modules/bulk.js";
import { initUndoDelete } from "./modules/undo.js";
import { initFilterToolbar } from "./modules/filters.js";
import { initSavedViews } from "./modules/saved-views.js";
import { initShortcutsHint } from "./modules/onboarding.js";
import { initProfilePage } from "./modules/profile.js";
import { initHomePage } from "./modules/home-init.js";
import { initNavigation } from "./modules/navigation.js";
import { initCalendarPage } from "./modules/calendar.js";

function configureHtmxCSP() {
  if (typeof htmx === "undefined") return;
  const nonceEl = document.querySelector("script[nonce]");
  const nonce = nonceEl && nonceEl.getAttribute("nonce");
  if (nonce) {
    htmx.config.inlineScriptNonce = nonce;
    htmx.config.allowEval = false;
  }
}
configureHtmxCSP();

// Expose these to global scope for HTMX and other inline scripts
window.apiPath = apiPath;
window.showToast = showToast;
window.closeSidebar = closeSidebar;
window.openSidebar = openSidebar;

document.addEventListener("DOMContentLoaded", () => {
  // Initialize all modules
  initTheme();
  attachThemeToggle();
  initCharacterCounters();
  initializeProjectFormHandlers();
  initializeProjectRenameHandlers();
  initializeTagRenameHandlers();
  initDueDatePresets();
  initializeSidebarEventListeners();
  attachSortableInitializers();
  initializeModalEventListeners();
  attachChangelogListener();
  attachNotificationListeners();
  attachAllEventListeners();
  initGlobalAnnouncement();
  initAnnouncementCharCounter();
  initDescriptionToggles();
  initBulkActions();
  initUndoDelete();
  initKeyboardShortcuts();
  initFilterToolbar();
  initSavedViews();
  initShortcutsHint();
  initNavigation();
  if (document.querySelector('main[data-page="home"]')) {
    initHomePage();
  }
  if (document.querySelector('main[data-page="profile"]')) {
    initProfilePage();
  }
  if (document.querySelector('main[data-page="calendar"]')) {
    initCalendarPage();
  }
  initRevealTokenButtons();

  // Debug helper: when ?cssdebug=1 is present in the URL, log which media queries match.
  (function cssDebugHelper() {
    try {
      const params = new URLSearchParams(window.location.search);
      if (!params.get("cssdebug")) return;

      const queries = {
        "max-420": "(max-width: 420px)",
        "max-600": "(max-width: 600px)",
        "max-768": "(max-width: 768px)",
        "max-1024": "(max-width: 1024px)",
        "pointer-coarse": "(pointer: coarse)",
        "hover-none": "(hover: none)",
      };

      console.groupCollapsed("CSS Debug — media query matches");
      Object.entries(queries).forEach(([k, q]) => {
        try {
          const m = window.matchMedia(q);
          console.log(q + ":", m.matches);
        } catch (e) {
          console.log(q + ": error");
        }
      });
      // Also log touch-capability and maxTouchPoints
      console.log("navigator.maxTouchPoints:", navigator.maxTouchPoints);
      console.log("ontouchstart in window:", "ontouchstart" in window);
      console.groupEnd();
    } catch (e) {}
  })();
});

function initRevealTokenButtons() {
  document.body.addEventListener("click", (e) => {
    const btn = e.target.closest(".reveal-token-btn");
    if (!btn) return;
    const cell = btn.closest("td");
    const code = cell && cell.querySelector(".token-masked");
    if (code && code.dataset.token) {
      code.textContent = code.dataset.token;
      btn.remove();
    }
  });
}
