import { apiPath } from "./utils.js";

let modalFocusHandler = null;
let lastFocusedBeforeModal = null;

function getModalFocusable(modalEl) {
  return Array.from(
    modalEl.querySelectorAll(
      'button:not([disabled]), [href], input:not([type="hidden"]):not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  ).filter((el) => el.offsetParent !== null || el === document.activeElement);
}

function attachModalFocusTrap(modalEl) {
  if (!modalEl || modalEl.classList.contains("modal-focus-initialized")) return;
  modalEl.classList.add("modal-focus-initialized");

  modalEl.addEventListener("show.bs.modal", () => {
    lastFocusedBeforeModal = document.activeElement;
  });

  modalEl.addEventListener("shown.bs.modal", () => {
    const focusables = getModalFocusable(modalEl);
    const first = focusables[0];
    if (first) first.focus();

    if (modalFocusHandler) {
      document.removeEventListener("keydown", modalFocusHandler);
    }
    modalFocusHandler = (e) => {
      if (e.key !== "Tab") return;
      const items = getModalFocusable(modalEl);
      if (items.length === 0) return;
      const firstEl = items[0];
      const lastEl = items[items.length - 1];
      if (e.shiftKey && document.activeElement === firstEl) {
        e.preventDefault();
        lastEl.focus();
      } else if (!e.shiftKey && document.activeElement === lastEl) {
        e.preventDefault();
        firstEl.focus();
      }
    };
    document.addEventListener("keydown", modalFocusHandler);
  });

  modalEl.addEventListener("hide.bs.modal", () => {
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
    }
  });

  modalEl.addEventListener("hidden.bs.modal", () => {
    modalEl.setAttribute("aria-hidden", "true");
    if (modalFocusHandler) {
      document.removeEventListener("keydown", modalFocusHandler);
      modalFocusHandler = null;
    }
    if (
      lastFocusedBeforeModal &&
      typeof lastFocusedBeforeModal.focus === "function"
    ) {
      try {
        lastFocusedBeforeModal.focus();
      } catch (e) {}
    }
  });
}

export function initializeModalEventListeners() {
  ["modal", "loginmodal", "shortcutsModal"].forEach((id) => {
    const modalEl = document.getElementById(id);
    if (modalEl) attachModalFocusTrap(modalEl);
  });

  const modalEl = document.getElementById("modal");
  if (!modalEl) return;
  // ensure bootstrap modal instance exists so data-bs-dismiss works
  try {
    if (
      typeof bootstrap !== "undefined" &&
      bootstrap.Modal &&
      typeof bootstrap.Modal.getOrCreateInstance === "function"
    ) {
      bootstrap.Modal.getOrCreateInstance(modalEl);
    }
  } catch (e) {}
}

export function renderChangelog(entries) {
  const container = document.getElementById("changelog-body");
  if (!container) return;
  if (!entries || !entries.length) {
    container.innerHTML =
      '<div class="text-center text-muted">No changelog entries available.</div>';
    return;
  }
  const out = document.createElement("div");
  out.className = "changelog-list";
  const MAX_MODAL = 5;
  const recent = entries.slice(0, MAX_MODAL);
  recent.forEach((e, idx) => {
    const card = document.createElement("div");
    card.className = "card mb-3";
    const cardBody = document.createElement("div");
    cardBody.className = "card-body";

    const headerBtn = document.createElement("button");
    headerBtn.type = "button";
    headerBtn.className =
      "btn btn-link text-start w-100 p-0 d-flex align-items-center";
    headerBtn.style.textDecoration = "none";

    const arrow = document.createElement("span");
    arrow.className = "chev me-2";
    arrow.textContent = "►";
    headerBtn.appendChild(arrow);

    const titleWrap = document.createElement("div");
    titleWrap.className = "flex-grow-1 text-start";
    const strong = document.createElement("strong");
    strong.textContent = e.title || "";
    titleWrap.appendChild(strong);

    const span = document.createElement("span");
    span.className =
      "badge releasetag ms-3 " +
      (e.prerelease ? "bg-warning text-dark" : "bg-success");
    span.textContent =
      (e.prerelease ? "Prerelease" : "Release") + " • " + (e.date || "");
    titleWrap.appendChild(span);

    headerBtn.appendChild(titleWrap);
    cardBody.appendChild(headerBtn);

    const collapseDiv = document.createElement("div");
    collapseDiv.id = `changelog-modal-${idx}-${Date.now()}`;
    collapseDiv.className = "collapse mt-2";

    if (e.html) {
      const bodyDiv = document.createElement("div");
      bodyDiv.className = "changelog-entry-body";
      bodyDiv.innerHTML = e.html;
      collapseDiv.appendChild(bodyDiv);
    } else {
      const ul = document.createElement("ul");
      if (Array.isArray(e.notes)) {
        e.notes.forEach((n) => {
          const li = document.createElement("li");
          li.textContent = n;
          ul.appendChild(li);
        });
      }
      collapseDiv.appendChild(ul);
    }

    headerBtn.addEventListener("click", function (ev) {
      ev.preventDefault();
      const opened = collapseDiv.classList.toggle("show");
      arrow.textContent = opened ? "▼" : "►";
    });

    cardBody.appendChild(collapseDiv);
    card.appendChild(cardBody);
    out.appendChild(card);
  });

  if (entries.length > recent.length) {
    const more = document.createElement("div");
    more.className = "text-center mt-3";
    const a = document.createElement("a");
    a.href = apiPath("/changelog/page");
    a.textContent = "View full changelog";
    more.appendChild(a);
    out.appendChild(more);
  }
  container.innerHTML = "";
  container.appendChild(out);
}

export function loadChangelog() {
  const container = document.getElementById("changelog-body");
  if (container)
    container.innerHTML =
      '<div class="text-center text-muted">Loading...</div>';
  fetch(apiPath("/changelog"))
    .then((res) => {
      if (!res.ok) throw new Error("failed to load changelog");
      return res.json();
    })
    .then((data) => {
      renderChangelog(data);
    })
    .catch((err) => {
      const container = document.getElementById("changelog-body");
      if (container)
        container.innerHTML =
          '<div class="text-danger">Unable to load changelog.</div>';
      console.error(err);
    });
}

export function attachChangelogListener() {
  const changelogModalEl = document.getElementById("changelogModal");
  if (changelogModalEl) {
    try {
      changelogModalEl.addEventListener("show.bs.modal", loadChangelog);
    } catch (e) {
      const link = document.querySelector('[data-bs-target="#changelogModal"]');
      if (link) link.addEventListener("click", loadChangelog);
    }
  }
}
