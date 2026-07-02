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

function isHelpKey(e) {
  return e.key === "?" || (e.code === "Slash" && e.shiftKey);
}

function isSearchKey(e) {
  return e.code === "Slash" && !e.shiftKey;
}

export function initKeyboardShortcuts() {
  window.addEventListener(
    "keydown",
    (e) => {
      if (e.defaultPrevented) return;
      if (e.ctrlKey || e.metaKey || e.altKey) return;

      const active = document.activeElement;
      const typing = isTypingTarget(active);

      if (e.code === "Escape") {
        closeOpenModals();
        if (typeof window.closeSidebar === "function") {
          window.closeSidebar();
        }
        return;
      }

      if (typing) return;

      if (isHelpKey(e)) {
        e.preventDefault();
        openShortcutsModal();
        return;
      }

      if (isSearchKey(e)) {
        const search = document.getElementById("search");
        if (search) {
          e.preventDefault();
          search.focus();
          if (typeof search.select === "function") search.select();
        }
        return;
      }

      if (e.code === "KeyN") {
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

      if (e.code === "KeyJ") {
        e.preventDefault();
        idx = idx < rows.length - 1 ? idx + 1 : 0;
        setFocusedRow(rows[idx]);
        return;
      }

      if (e.code === "KeyK") {
        e.preventDefault();
        idx = idx > 0 ? idx - 1 : rows.length - 1;
        setFocusedRow(rows[idx]);
        return;
      }

      if (!focused) return;

      if (e.code === "KeyE") {
        e.preventDefault();
        const editBtn = focused.querySelector(".edit-btn");
        if (editBtn) editBtn.click();
        return;
      }

      if (e.code === "KeyX") {
        e.preventDefault();
        const statusBtn = focused.querySelector(".status-column");
        if (statusBtn) statusBtn.click();
      }
    },
    true,
  );
}
