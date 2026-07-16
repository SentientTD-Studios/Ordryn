import { initTheme, attachThemeToggle } from "./modules/theme.js";
import { initProfilePage } from "./modules/profile.js";
import { initNavigation } from "./modules/navigation.js";

document.addEventListener("DOMContentLoaded", () => {
  initTheme();
  attachThemeToggle();
  initProfilePage();
  initNavigation();
});
