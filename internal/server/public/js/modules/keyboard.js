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

let lastFocusedTaskId = null;
let pendingFocusTaskId = null;

function taskIdFromRow(row) {
  if (!row) return null;
  if (row.id && row.id.startsWith("task-")) {
    return row.id.slice("task-".length);
  }
  return row.getAttribute("data-task-id");
}

function rowForTaskId(taskId) {
  if (!taskId) return null;
  return document.getElementById(`task-${taskId}`);
}

function setFocusedRow(row) {
  document.querySelectorAll("tr.task-row-focused").forEach((r) => {
    r.classList.remove("task-row-focused");
  });
  if (!row) {
    lastFocusedTaskId = null;
    return;
  }
  row.classList.add("task-row-focused");
  lastFocusedTaskId = taskIdFromRow(row);
  row.focus({ preventScroll: true });
  row.scrollIntoView({ block: "nearest" });
}

function getFocusedRow() {
  return document.querySelector("tr.task-row-focused");
}

function resolveFocusedRow() {
  const focused = getFocusedRow();
  if (focused) return focused;
  return rowForTaskId(lastFocusedTaskId);
}

function resolveRowIndex(rows) {
  const row = resolveFocusedRow();
  if (!row) return -1;
  return rows.indexOf(row);
}

function attachKeyboardFocusRestore() {
  document.body.addEventListener("htmx:beforeSwap", (evt) => {
    const target = evt.detail?.target;
    if (!target?.id?.startsWith("task-")) return;
    const focused = getFocusedRow();
    if (focused && focused.id === target.id) {
      pendingFocusTaskId = taskIdFromRow(target);
    }
  });

  document.body.addEventListener("htmx:afterSwap", (evt) => {
    const target = evt.detail?.target || evt.target;
    if (!target?.id?.startsWith("task-")) return;
    if (pendingFocusTaskId && taskIdFromRow(target) === pendingFocusTaskId) {
      setFocusedRow(target);
      pendingFocusTaskId = null;
    }
  });
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
  attachKeyboardFocusRestore();

  window.addEventListener(
    "keydown",
    (e) => {
      if (e.defaultPrevented) return;
      if (e.ctrlKey || e.metaKey || e.altKey) return;

      const active = document.activeElement;
      const typing = isTypingTarget(active);

      if (e.code === "Escape") {
        closeOpenModals();
        if (!typing && typeof window.closeSidebar === "function") {
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

      let focused = resolveFocusedRow();
      let idx = resolveRowIndex(rows);

      if (e.code === "KeyK") {
        e.preventDefault();
        idx = idx < rows.length - 1 ? idx + 1 : 0;
        setFocusedRow(rows[idx]);
        return;
      }

      if (e.code === "KeyJ") {
        e.preventDefault();
        idx = idx > 0 ? idx - 1 : rows.length - 1;
        setFocusedRow(rows[idx]);
        return;
      }

      if (!focused) return;

      if (e.code === "Enter") {
        e.preventDefault();
        const editBtn = focused.querySelector(".edit-btn");
        if (editBtn) editBtn.click();
        return;
      }

      if (e.code === "KeyD") {
        e.preventDefault();
        const deleteBtn = focused.querySelector(".delete-btn");
        if (deleteBtn) deleteBtn.click();
        return;
      }

      if (e.code === "KeyE") {
        e.preventDefault();
        const editBtn = focused.querySelector(".edit-btn");
        if (editBtn) editBtn.click();
        return;
      }

      if (e.code === "KeyX") {
        e.preventDefault();
        if (!focused.classList.contains("task-row-focused")) {
          setFocusedRow(focused);
        }
        const statusBtn = focused.querySelector(".status-column");
        if (statusBtn) statusBtn.click();
      }
    },
    true,
  );
}
