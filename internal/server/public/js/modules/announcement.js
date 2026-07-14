import { apiPath } from "./utils.js";

export function dismissGlobalAnnouncement() {
  const announcement = document.getElementById("global-announcement");
  if (announcement) {
    // Add fade-out animation
    announcement.classList.add("fade-out");

    // Remove from DOM after animation completes
    setTimeout(() => {
      announcement.remove();
    }, 300);

    // Store dismissal on server
    fetch(apiPath('/api/dismiss-announcement'), {
      method: 'POST',
      credentials: 'same-origin' // Include session cookie
    }).catch(err => console.error('Failed to dismiss announcement:', err));
  }
}

export function initGlobalAnnouncement() {
  const announcement = document.getElementById("global-announcement");
  if (announcement) {
    const closeButton = announcement.querySelector(".btn-close");
    if (closeButton) {
      closeButton.addEventListener("click", dismissGlobalAnnouncement);
    }
  }
}