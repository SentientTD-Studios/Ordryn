import { ensureToastContainer } from "./utils.js";
import { apiPath } from "./utils.js";

export function showToast(message, opts) {
  opts = opts || {};
  const container = ensureToastContainer();
  const t = document.createElement("div");
  t.className = "app-toast" + (opts.error ? " app-toast--error" : "");
  t.setAttribute("role", opts.error ? "alert" : "status");
  t.setAttribute("aria-live", opts.error ? "assertive" : "polite");

  const text = document.createElement("span");
  text.className = "app-toast-text";
  text.textContent = message;
  t.appendChild(text);

  const remove = () => {
    t.classList.remove("show");
    setTimeout(() => {
      try {
        t.remove();
      } catch (e) {}
    }, 220);
  };

  let to = null;
  const scheduleRemove = (ms) => {
    if (to) clearTimeout(to);
    to = setTimeout(remove, ms);
  };

  if (opts.actionLabel && typeof opts.onAction === "function") {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "btn btn-sm btn-link app-toast-action";
    btn.textContent = opts.actionLabel;
    btn.addEventListener("click", (e) => {
      e.stopPropagation();
      opts.onAction();
      scheduleRemove(0);
      remove();
    });
    t.appendChild(btn);
  }

  const closeBtn = document.createElement("button");
  closeBtn.type = "button";
  closeBtn.className = "btn-close app-toast-close";
  closeBtn.setAttribute("aria-label", "Dismiss notification");
  closeBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    scheduleRemove(0);
    remove();
  });
  t.appendChild(closeBtn);

  container.appendChild(t);

  requestAnimationFrame(() => {
    t.classList.add("show");
  });

  const timeout =
    typeof opts.duration === "number"
      ? opts.duration
      : opts.error
        ? 8000
        : 3500;
  scheduleRemove(timeout);

  t.addEventListener("click", (e) => {
    if (e.target.closest(".app-toast-action, .app-toast-close")) return;
    scheduleRemove(0);
    remove();
  });
}

export function attachNotificationListeners() {
  document.body.addEventListener("import-complete", () => {
    showToast("Import completed.");
  });

  document.body.addEventListener("tags-changed", () => {
    showToast("Tags updated.");
    refreshTagFilterSelect();
  });

  document.body.addEventListener("htmx:afterRequest", (evt) => {
    const xhr = evt.detail?.xhr;
    if (!xhr?.getResponseHeader) return;
    const trigger = xhr.getResponseHeader("HX-Trigger");
    if (!trigger) return;
    if (trigger.indexOf("task-deleted") !== -1) return;
    try {
      const parsed = JSON.parse(trigger);
      if (parsed["import-complete"]) {
        document.body.dispatchEvent(new CustomEvent("import-complete"));
      }
      if (parsed["tags-changed"]) {
        document.body.dispatchEvent(new CustomEvent("tags-changed"));
      }
    } catch (e) {
      if (trigger.indexOf("import-complete") !== -1) {
        document.body.dispatchEvent(new CustomEvent("import-complete"));
      }
      if (trigger.indexOf("tags-changed") !== -1) {
        document.body.dispatchEvent(new CustomEvent("tags-changed"));
      }
    }
  });
}

function refreshTagFilterSelect() {
  fetch(apiPath("/api/tags/json"))
    .then((res) => (res.ok ? res.json() : []))
    .then((data) => {
      const sel = document.getElementById("tag-filter-toolbar");
      if (!sel) return;
      const cur = sel.value;
      while (sel.options.length > 1) sel.remove(1);
      data.forEach((t) => {
        const opt = document.createElement("option");
        opt.value = String(t.id);
        opt.textContent = t.name;
        sel.appendChild(opt);
      });
      try {
        sel.value = cur;
      } catch (e) {}
      const hidden = document.getElementById("tag-filter");
      if (hidden) hidden.value = sel.value;
    })
    .catch(() => {});
}
