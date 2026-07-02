function isTypingTarget(el) {
  if (!el || !(el instanceof Element)) return false;
  const tag = el.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (el.isContentEditable) return true;
  return !!el.closest("[contenteditable='true']");
}

function getTaskRows() {
  const container = document.getElementById("task-container");
  if (!container) return [];
  return Array.from(container.querySelectorAll("tr.task-row"));
}

function setFocusedRow(row) {
  document.querySelectorAll("tr.task-row-focused").forEach((r) => {
    r.classList.remove("task-row-focused");
  });
  if (!row) return;
  row.classList.add("task-row-focused");
  row.focus({ preventScroll: true });
  row.scrollIntoView({ block: "nearest" });
}

function getFocusedRow() {
  return document.querySelector("tr.task-row-focused");
}

function openShortcutsModal() {
  const el = document.getElementById("shortcutsModal");
  if (!el || typeof bootstrap === "undefined") return;
  bootstrap.Modal.getOrCreateInstance(el).show();
}

function closeOpenModals() {
  if (typeof bootstrap === "undefined" || !bootstrap.Modal) return;
  ["modal", "loginmodal", "shortcutsModal", "changelogModal"].forEach((id) => {
    const el = document.getElementById(id);
    if (!el) return;
    const inst = bootstrap.Modal.getInstance(el);
    if (inst) inst.hide();
  });
}

export function initKeyboardShortcuts() {
  document.body.addEventListener("keydown", (e) => {
    if (e.defaultPrevented) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;

    const active = document.activeElement;
    const typing = isTypingTarget(active);

    if (e.key === "Escape") {
      closeOpenModals();
      if (typeof window.closeSidebar === "function") {
        window.closeSidebar();
      }
      return;
    }

    if (typing) return;

    if (e.key === "?" || (e.key === "/" && e.shiftKey)) {
      e.preventDefault();
      openShortcutsModal();
      return;
    }

    if (e.key === "/") {
      const search = document.getElementById("search");
      if (search) {
        e.preventDefault();
        search.focus();
        search.select();
      }
      return;
    }

    if (e.key === "n") {
      const openBtn = document.getElementById("openSidebar");
      if (openBtn) {
        e.preventDefault();
        openBtn.click();
      }
      return;
    }

    const rows = getTaskRows();
    if (rows.length === 0) return;

    let focused = getFocusedRow();
    let idx = focused ? rows.indexOf(focused) : -1;

    if (e.key === "j") {
      e.preventDefault();
      idx = idx < rows.length - 1 ? idx + 1 : 0;
      setFocusedRow(rows[idx]);
      return;
    }

    if (e.key === "k") {
      e.preventDefault();
      idx = idx > 0 ? idx - 1 : rows.length - 1;
      setFocusedRow(rows[idx]);
      return;
    }

    if (!focused) return;

    if (e.key === "e") {
      e.preventDefault();
      const editBtn = focused.querySelector(".edit-btn");
      if (editBtn) editBtn.click();
      return;
    }

    if (e.key === "x") {
      e.preventDefault();
      const statusBtn = focused.querySelector(".status-column");
      if (statusBtn) statusBtn.click();
    }
  });
}
