/** Shared filter field serialization for bulk update and undo requests. */
export function appendFilterFields(form) {
  const append = (name, hiddenId, toolbarId) => {
    let val = "";
    if (toolbarId) {
      const toolbar = document.getElementById(toolbarId);
      if (toolbar) val = toolbar.value;
    }
    if (!val && hiddenId) {
      const hidden = document.getElementById(hiddenId);
      if (hidden) val = hidden.value;
    }
    if (val) form.append(name, val);
  };
  append("project", "project-filter-value", "project-filter");
  append("status", "status-filter", "status-filter-select");
  append("due", "due-filter", null);
  append("sort", "sort-filter", null);
  append("priority", "priority-filter", "priority-filter-toolbar");
  append("tag", "tag-filter", "tag-filter-toolbar");
  const search = document.getElementById("search");
  if (search && search.value) form.append("search", search.value);
}
