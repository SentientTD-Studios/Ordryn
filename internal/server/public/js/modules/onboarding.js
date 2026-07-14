import { showToast } from "./notifications.js";

export function initShortcutsHint() {
  const btn = document.getElementById("shortcuts-help-btn");
  if (!btn) return;
  if (localStorage.getItem("shortcuts-hint-dismissed") === "1") return;

  setTimeout(() => {
    showToast("Tip: Press ? for keyboard shortcuts.", {
      duration: 6000,
      actionLabel: "Got it",
      onAction: () => {
        localStorage.setItem("shortcuts-hint-dismissed", "1");
      },
    });
  }, 1500);
}
