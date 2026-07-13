// Admin page functionality
// Handles character counter and validation for admin settings

/**
 * Initialize character counter for global announcement text
 */
export function initAnnouncementCharCounter() {
  const announcementText = document.getElementById("global_announcement_text");
  const announcementCharCount = document.getElementById(
    "announcement-char-count",
  );

  if (announcementText && announcementCharCount) {
    if (announcementText.dataset.counterBound !== "true") {
      announcementText.dataset.counterBound = "true";
      announcementText.addEventListener("input", function () {
        const length = this.value.length;
        announcementCharCount.textContent = length;

        if (length > 450) {
          announcementCharCount.classList.add("text-warning");
        } else {
          announcementCharCount.classList.remove("text-warning");
        }
        if (length > 480) {
          announcementCharCount.classList.add("text-danger");
        } else {
          announcementCharCount.classList.remove("text-danger");
        }

        const errorDiv = document.getElementById("announcement-text-error");
        if (errorDiv) {
          errorDiv.innerHTML = "";
        }
      });
    }
  }
}
