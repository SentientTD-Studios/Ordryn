import { apiPath } from "./utils.js";
import { attachThemeToggle, initTheme } from "./theme.js";
import { handleDescriptionInput } from "./form-handlers.js";

export function openSidebar() {
  const sidebar = document.getElementById("sidebar");
  if (sidebar) {
    sidebar.classList.add("active");
  }
}

export function closeSidebar() {
  const sidebar = document.getElementById("sidebar");
  if (sidebar) {
    sidebar.classList.remove("active");
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
          // Ensure the form posts to the add endpoint
          try {
            tf.setAttribute("hx-post", apiPath("/api/add-task"));
          } catch (e) {}
          const cp = tf.querySelector('input[name="currentPage"]');
          if (cp) cp.value = "1";
          // Ensure the form carries the current toolbar project filter so server can decide refresh
          try {
            let projField = tf.querySelector('input[name="project"]');
            let statusField = tf.querySelector('input[name="status"]');
            const toolbar = document.querySelector("select#project-filter");
            const toolbarVal = toolbar ? toolbar.value : "";
            const statusFilter = document.getElementById("status-filter");
            const statusVal = statusFilter ? statusFilter.value : "";
            if (!projField) {
              projField = document.createElement("input");
              projField.type = "hidden";
              projField.name = "project";
              tf.appendChild(projField);
            }
            if (!statusField) {
              statusField = document.createElement("input");
              statusField.type = "hidden";
              statusField.name = "status";
              tf.appendChild(statusField);
            }
            projField.value = toolbarVal;
            statusField.value = statusVal;
          } catch (e) {}
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

  // Reattach task form submit listener so dynamically swapped forms behave the same
  try {
    const tf = document.getElementById("newTaskForm");
    if (tf && !tf.classList.contains("task-form-initialized")) {
      // Ensure hidden project field exists and is kept up-to-date before submit
      try {
        let projField = tf.querySelector('input[name="project"]');
        let statusField = tf.querySelector('input[name="status"]');
        const toolbar = document.querySelector("select#project-filter");
        const toolbarVal = toolbar ? toolbar.value : "";
        const statusFilter = document.getElementById("status-filter");
        const statusVal = statusFilter ? statusFilter.value : "";
        if (!projField) {
          projField = document.createElement("input");
          projField.type = "hidden";
          projField.name = "project";
          tf.appendChild(projField);
        }
        if (!statusField) {
          statusField = document.createElement("input");
          statusField.type = "hidden";
          statusField.name = "status";
          tf.appendChild(statusField);
        }
        projField.value = toolbarVal;
        statusField.value = statusVal;
        // Update it on submit in case toolbar changed while form open
        tf.addEventListener("submit", function () {
          try {
            const tb = document.querySelector("select#project-filter");
            if (tb) projField.value = tb.value;
            const sf = document.getElementById("status-filter");
            statusField.value = sf ? sf.value : "";
          } catch (e) {}
        });
      } catch (e) {}
      tf.addEventListener("htmx:afterRequest", (event) => {
        let isValidationError = false;
        try {
          const xhr = event.detail && event.detail.xhr;
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
          const tEl = document.getElementById("title");
          if (tEl) tEl.value = "";
          const dEl = document.getElementById("description");
          if (dEl) dEl.value = "";
          const charCount = document.getElementById("char-count");
          if (charCount) charCount.textContent = "0";
          const errorDiv = document.getElementById("description-error");
          if (errorDiv) errorDiv.innerHTML = "";
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
    const sb = document.getElementById("sidebar");
    if (sb) sb.classList.add("active");
  } catch (e) {}
}

export function attachContextualCloseSidebar() {
  // Delegated close button handler: works even if the sidebar markup was swapped
  document.body.removeEventListener("click", handleSidebarCloseClick);
  document.body.addEventListener("click", handleSidebarCloseClick);
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
  // Check if the sidebar element exists and is currently active
  const sidebarElement = document.getElementById("sidebar");
  if (sidebarElement && sidebarElement.classList.contains("active")) {
    // Re-initialize character counter if elements are present
    let description = document.getElementById("description");
    let charCount = document.getElementById("char-count");
    if (description && charCount) {
      handleDescriptionInput(charCount);
    }
    // Re-initialize theme toggle if needed
    if (typeof initTheme === "function") {
      initTheme();
    }
  }
}
