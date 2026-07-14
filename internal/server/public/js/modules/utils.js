// Helper to build correct API URLs that work on localhost, mounted subpaths, and /p/{project} URLs.
let cachedBasePath = null;

function detectBasePath() {
  if (cachedBasePath !== null) return cachedBasePath;

  const asset = document.querySelector(
    'script[src*="/public/"], link[href*="/public/"]',
  );
  const assetPath =
    asset?.getAttribute("src") || asset?.getAttribute("href") || "";

  if (assetPath) {
    try {
      const path = new URL(assetPath, window.location.origin).pathname;
      const publicIdx = path.indexOf("/public/");
      if (publicIdx > 0) {
        cachedBasePath = path.slice(0, publicIdx);
        return cachedBasePath;
      }
    } catch (e) {
      /* fall through to root */
    }
  }

  cachedBasePath = "";
  return cachedBasePath;
}

export function apiPath(endpoint) {
  const path = endpoint.startsWith("/") ? endpoint : `/${endpoint}`;
  return detectBasePath() + path;
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
