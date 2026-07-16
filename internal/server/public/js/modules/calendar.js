import { openSidebar } from "./sidebar.js";
import { bindDueDatePresets } from "./form-handlers.js";

const MONTH_LABELS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
];

const MONTH_FULL = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];

function prepareAddTaskForm(dueDate) {
  const tf = document.getElementById("newTaskForm");
  if (!tf) return;

  const titleEl = tf.querySelector("#title");
  if (titleEl) titleEl.value = "";
  const descEl = tf.querySelector("#description");
  if (descEl) descEl.value = "";
  const projEl = tf.querySelector("#project_id");
  if (projEl) projEl.value = "";
  const dueEl = tf.querySelector("#due_date");
  if (dueEl) dueEl.value = dueDate || "";

  const idInput = tf.querySelector('input[name="id"]');
  if (idInput) idInput.remove();

  const submit = tf.querySelector('button[type="submit"]');
  if (submit) submit.textContent = "Add Task";

  tf.setAttribute("hx-post", tf.getAttribute("hx-post") || "");

  const sbTitle = document.querySelector("#sidebar .sidebar-header h5");
  if (sbTitle) sbTitle.textContent = "Add Task";

  const charCount = document.getElementById("char-count");
  if (charCount) charCount.textContent = "0";

  const newTagsEl = tf.querySelector("#new_tags");
  if (newTagsEl) newTagsEl.value = "";
  tf.querySelectorAll('input[name="tag_ids"]').forEach((cb) => {
    cb.checked = false;
  });

  bindDueDatePresets(tf);
}

function parseYearMonth(ym) {
  if (!ym || ym.length !== 7) {
    return { year: new Date().getFullYear(), month: 1 };
  }
  const [y, m] = ym.split("-");
  return { year: parseInt(y, 10), month: parseInt(m, 10) };
}

function submitCalendarMonth(base, ym) {
  const form = document.createElement("form");
  form.method = "POST";
  form.action = `${base}/calendar`;
  const input = document.createElement("input");
  input.type = "hidden";
  input.name = "month";
  input.value = ym;
  form.appendChild(input);
  document.body.appendChild(form);
  form.submit();
}

function initCalendarJumpPicker(page) {
  const toggleLabel = document.getElementById("calendar-jump-label");
  const menu = document.getElementById("calendar-jump-menu");
  const yearLabel = document.getElementById("calendar-jump-year");
  const monthsGrid = document.getElementById("calendar-jump-months");
  const yearPrev = document.getElementById("calendar-jump-year-prev");
  const yearNext = document.getElementById("calendar-jump-year-next");

  if (!menu || !yearLabel || !monthsGrid) return;

  const base = page.dataset.calendarBase || "";
  const view = parseYearMonth(page.dataset.calendarMonth || "");

  let pickerYear = view.year;
  let pickerMonth = view.month;

  const updateTogglePreview = () => {
    if (!toggleLabel) return;
    toggleLabel.textContent = `${MONTH_FULL[pickerMonth - 1]} ${pickerYear}`;
  };

  const renderMonths = () => {
    yearLabel.textContent = String(pickerYear);
    monthsGrid.innerHTML = "";

    MONTH_LABELS.forEach((label, i) => {
      const month = i + 1;
      const ym = `${pickerYear}-${String(month).padStart(2, "0")}`;
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "calendar-jump-month";
      btn.textContent = label;
      btn.setAttribute("aria-label", `${MONTH_FULL[i]} ${pickerYear}`);

      if (pickerYear === view.year && month === view.month) {
        btn.classList.add("calendar-jump-month--active");
      }
      if (ym === page.dataset.calendarToday) {
        btn.classList.add("calendar-jump-month--today");
      }

      btn.addEventListener("click", () => {
        pickerMonth = month;
        updateTogglePreview();
        submitCalendarMonth(base, ym);
      });

      monthsGrid.appendChild(btn);
    });

    updateTogglePreview();
  };

  const stopClose = (e) => e.stopPropagation();

  yearPrev?.addEventListener("click", (e) => {
    stopClose(e);
    pickerYear -= 1;
    renderMonths();
  });

  yearNext?.addEventListener("click", (e) => {
    stopClose(e);
    pickerYear += 1;
    renderMonths();
  });

  const dropdown = document.querySelector(".calendar-jump.dropdown");
  dropdown?.addEventListener("show.bs.dropdown", () => {
    pickerYear = view.year;
    pickerMonth = view.month;
    renderMonths();
  });

  menu.addEventListener("click", stopClose);
}

export function initCalendarPage() {
  const page =
    document.querySelector('main[data-page="calendar"]') ||
    document.querySelector(".calendar-page");
  if (!page) return;

  initCalendarJumpPicker(page);

  const addBtn = document.getElementById("calendar-add-task");
  if (addBtn) {
    addBtn.addEventListener("click", () => {
      prepareAddTaskForm("");
      openSidebar();
      const titleEl = document.getElementById("title");
      if (titleEl) titleEl.focus();
    });
  }

  page.addEventListener("click", (e) => {
    const dayBtn = e.target.closest(".calendar-add-btn");
    if (!dayBtn) return;
    e.preventDefault();
    prepareAddTaskForm(dayBtn.getAttribute("data-due-date") || "");
    openSidebar();
    const titleEl = document.getElementById("title");
    if (titleEl) titleEl.focus();
  });
}
