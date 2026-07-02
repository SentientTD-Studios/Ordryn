export function initCharacterCounters() {
  const description = document.getElementById("description");
  const charCount = document.getElementById("char-count");

  // Character counter for description
  if (description && charCount) {
    description.addEventListener("input", function () {
      const length = this.value.length;
      charCount.textContent = length;

      // Add visual feedback when approaching limit
      if (length > 900) {
        charCount.classList.add("text-warning");
      } else {
        charCount.classList.remove("text-warning");
      }
      if (length > 950) {
        charCount.classList.add("text-danger");
      } else {
        charCount.classList.remove("text-danger");
      }
    });
  }

  // Character counter for project name
  const projectNameInput = document.getElementById("project-name");
  const projectNameCharCount = document.getElementById(
    "project-name-char-count",
  );
  if (projectNameInput && projectNameCharCount) {
    projectNameInput.addEventListener("input", function () {
      const length = this.value.length;
      projectNameCharCount.textContent = length;

      // Add visual feedback when approaching limit
      if (length > 40) {
        projectNameCharCount.classList.add("text-warning");
      } else {
        projectNameCharCount.classList.remove("text-warning");
      }
      if (length > 45) {
        projectNameCharCount.classList.add("text-danger");
      } else {
        projectNameCharCount.classList.remove("text-danger");
      }
    });
  }
}

export function initializeProjectFormHandlers() {
  const projectForm = document.getElementById("createProjectForm");
  const projectNameInput = document.getElementById("project-name");
  const projectNameError = document.getElementById("project-name-error");

  if (projectForm && projectNameInput && projectNameError) {
    // Clear error when user starts typing
    projectNameInput.addEventListener("input", function () {
      projectNameError.innerHTML = "";
    });

    // Handle validation errors from server
    projectForm.addEventListener("htmx:afterRequest", function (event) {
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
          event.detail.triggerSpec.trigger === "project-name-error"
        ) {
          isValidationError = true;
        }
      } catch (e) {}

      // Clear form on successful submission (not validation error)
      if (event.detail.successful && !isValidationError) {
        projectNameInput.value = "";
        const charCount = document.getElementById("project-name-char-count");
        if (charCount) charCount.textContent = "0";
        projectNameError.innerHTML = "";
      }
    });
  }
}

export function initializeProjectRenameHandlers() {
  document.body.addEventListener("click", function (e) {
    const editBtn = e.target.closest(".edit-project-btn");
    if (editBtn) {
      const td = editBtn.closest("td");
      if (!td) return;
      td.querySelector(".project-name-display")?.classList.add("d-none");
      td.querySelector(".project-rename-form")?.classList.remove("d-none");
      editBtn.classList.add("d-none");
      td.querySelector('.project-rename-form input[name="name"]')?.focus();
      return;
    }

    const cancelBtn = e.target.closest(".cancel-rename-btn");
    if (!cancelBtn) return;
    const td = cancelBtn.closest("td");
    if (!td) return;
    td.querySelector(".project-name-display")?.classList.remove("d-none");
    td.querySelector(".project-rename-form")?.classList.add("d-none");
    td.querySelector(".edit-project-btn")?.classList.remove("d-none");
  });
}

export function handleDescriptionInput(charCountElement) {
  const description = document.getElementById("description");
  if (!description || !charCountElement) return;

  // Initialize count
  charCountElement.textContent = description.value.length;

  // Check if listener already added
  if (!description.classList.contains("char-count-listener-added")) {
    description.addEventListener("input", function () {
      const length = this.value.length;
      charCountElement.textContent = length;

      // Add visual feedback when approaching limit
      if (length > 900) {
        charCountElement.classList.add("text-warning");
      } else {
        charCountElement.classList.remove("text-warning");
      }
      if (length > 950) {
        charCountElement.classList.add("text-danger");
      } else {
        charCountElement.classList.remove("text-danger");
      }
    });
    description.classList.add("char-count-listener-added");
  }
}
