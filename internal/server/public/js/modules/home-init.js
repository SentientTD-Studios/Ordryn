/** Home page boot helpers (project filter restore, auth flash URL cleanup). */
let homeListenersBound = false;

function getHomeMain() {
  return document.querySelector('main[data-page="home"]');
}

export function initHomePage(mainEl) {
  const main = mainEl || getHomeMain();
  if (!main) return;

  const projectFilter = main.dataset.projectFilter;
  if (projectFilter !== undefined) {
    const sel = document.getElementById("project-filter");
    if (sel) sel.value = projectFilter;
  }

  const urlParams = new URLSearchParams(window.location.search);
  if (
    urlParams.get("logged_out") === "true" ||
    urlParams.get("account_created") === "true"
  ) {
    urlParams.delete("logged_out");
    urlParams.delete("account_created");
    const newUrl =
      window.location.pathname +
      (urlParams.toString() ? "?" + urlParams.toString() : "");
    window.history.replaceState({}, "", newUrl);
    setTimeout(() => {
      const alert = document.querySelector("#status");
      if (alert && typeof bootstrap !== "undefined") {
        const bsAlert = bootstrap.Alert.getOrCreateInstance(alert);
        bsAlert.close();
      }
    }, 5000);
  }

  if (!homeListenersBound) {
    homeListenersBound = true;

    document.body.addEventListener("htmx:afterSwap", (event) => {
      if (event.target.id === "status") {
        setTimeout(() => {
          event.target.style.display = "none";
        }, 5000);
      }
    });

    document.body.addEventListener("task-added", () => {
      const title = document.getElementById("title");
      const description = document.getElementById("description");
      if (title) title.value = "";
      if (description) description.value = "";
    });
  }
}
