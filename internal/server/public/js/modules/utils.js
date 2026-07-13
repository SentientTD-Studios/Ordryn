// Helper to build correct API URLs that work on both localhost and subpaths
export function apiPath(endpoint) {
  // Remove leading slash if present
  const path = endpoint.startsWith("/") ? endpoint.slice(1) : endpoint;
  // Use relative path with dot prefix so HTMX resolves it relative to current location
  return "./" + path;
}

export function ensureToastContainer() {
  let c = document.querySelector(".app-toast-container");
  if (!c) {
    c = document.createElement("div");
    c.className = "app-toast-container";
    document.body.appendChild(c);
  }
  return c;
}
