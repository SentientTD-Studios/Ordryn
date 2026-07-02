export function initDescriptionToggles() {
  document.body.addEventListener("click", (e) => {
    const showMore = e.target.closest(".desc-show-more");
    if (showMore) {
      e.preventDefault();
      e.stopPropagation();
      const cell = showMore.closest(".desc-column");
      if (!cell) return;
      cell.classList.add("desc-expanded");
      showMore.classList.add("d-none");
      const showLess = cell.querySelector(".desc-show-less");
      if (showLess) showLess.classList.remove("d-none");
      return;
    }

    const showLess = e.target.closest(".desc-show-less");
    if (showLess) {
      e.preventDefault();
      e.stopPropagation();
      const cell = showLess.closest(".desc-column");
      if (!cell) return;
      cell.classList.remove("desc-expanded");
      showLess.classList.add("d-none");
      const showMoreBtn = cell.querySelector(".desc-show-more");
      if (showMoreBtn) showMoreBtn.classList.remove("d-none");
      return;
    }

    const toggle = e.target.closest(".task-toggle");
    if (!toggle || e.target.closest(".favorite-btn, .edit-btn, .delete-column, .tag-chip")) {
      return;
    }
    const row = toggle.closest("tr");
    if (!row) return;
    const expanded = row.classList.toggle("expanded");
    toggle.setAttribute("aria-expanded", expanded ? "true" : "false");
  });

  document.body.addEventListener("keydown", (e) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    const toggle = e.target.closest(".task-toggle");
    if (!toggle) return;
    e.preventDefault();
    toggle.click();
  });
}
