import { apiPath } from "./utils.js";
import { swapTaskContainerHtml } from "./events.js";
import { showToast } from "./notifications.js";
import { appendFilterFields } from "./filter-fields.js";

const UNDO_TOAST_MS = 120000;

export function initUndoDelete() {
  document.body.addEventListener("task-deleted", (e) => {
    const detail = (e && e.detail) || {};
    const count = detail.count || 1;
    const label = count === 1 ? "1 task deleted" : `${count} tasks deleted`;

    showToast(label, {
      duration: UNDO_TOAST_MS,
      actionLabel: "Undo",
      onAction: () => {
        const form = new URLSearchParams();
        form.append("page", getCurrentPage());
        appendFilterFields(form);

        fetch(apiPath("/api/undo-delete"), {
          method: "POST",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded",
            "HX-Request": "true",
          },
          body: form.toString(),
        })
          .then((res) => {
            if (!res.ok) {
              return res.text().then((t) => {
                throw new Error(t || "Undo failed");
              });
            }
            return res.text();
          })
          .then((html) => {
            swapTaskContainerHtml(html);
            showToast("Delete undone.");
          })
          .catch((err) => {
            showToast(err.message || "Undo failed.", { error: true });
          });
      },
    });
  });

  document.body.addEventListener("htmx:afterRequest", (evt) => {
    const xhr = evt.detail && evt.detail.xhr;
    if (!xhr) return;
    const trigger = xhr.getResponseHeader("HX-Trigger");
    if (!trigger || trigger.indexOf("task-deleted") === -1) return;
    try {
      const parsed = JSON.parse(trigger);
      if (parsed["task-deleted"]) {
        document.body.dispatchEvent(
          new CustomEvent("task-deleted", { detail: parsed["task-deleted"] }),
        );
      }
    } catch (e) {
      document.body.dispatchEvent(
        new CustomEvent("task-deleted", { detail: { count: 1 } }),
      );
    }
  });
}

function getCurrentPage() {
  const pageInput = document.getElementById("current-page");
  if (pageInput && pageInput.value) {
    const page = parseInt(pageInput.value, 10);
    if (!Number.isNaN(page) && page > 0) return String(page);
  }
  return "1";
}
