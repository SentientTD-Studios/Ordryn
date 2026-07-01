import { ensureToastContainer } from "./utils.js";

export function showToast(message, opts) {
  opts = opts || {};
  const container = ensureToastContainer();
  const t = document.createElement("div");
  t.className = "app-toast" + (opts.error ? " app-toast--error" : "");
  t.setAttribute("role", "status");
  t.setAttribute("aria-live", "polite");
  t.textContent = message;
  container.appendChild(t);

  // ensure next frame for animation
  requestAnimationFrame(() => {
    t.classList.add("show");
  });

  const timeout = typeof opts.duration === "number" ? opts.duration : 3500;
  const remove = () => {
    t.classList.remove("show");
    setTimeout(() => {
      try {
        t.remove();
      } catch (e) {}
    }, 220);
  };

  // Auto-remove
  const to = setTimeout(remove, timeout);

  // Allow manual dismissal on click
  t.addEventListener("click", function () {
    clearTimeout(to);
    remove();
  });
}

export function attachNotificationListeners() {
  // Reserved for future server-driven notification triggers.
}
