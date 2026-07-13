import { attachThemeToggle, initTheme } from "./theme.js";

function configureHtmxCSP() {
  if (typeof htmx === "undefined") return;
  const nonceEl = document.querySelector("script[nonce]");
  const nonce = nonceEl && nonceEl.getAttribute("nonce");
  if (nonce) {
    htmx.config.inlineScriptNonce = nonce;
    htmx.config.allowEval = false;
  }
}
configureHtmxCSP();

function readDashboardConfig() {
  const el = document.getElementById("dashboard-config");
  if (!el) return null;
  try {
    return JSON.parse(el.textContent || "{}");
  } catch {
    return null;
  }
}

function destroyChartOnCanvas(canvas) {
  if (!canvas || typeof Chart === "undefined") return;
  const existing = Chart.getChart(canvas);
  if (existing) existing.destroy();
}

export function initDashboardCharts() {
  if (typeof Chart === "undefined") return;

  const config = readDashboardConfig();
  if (!config) return;

  const styles = getComputedStyle(document.documentElement);
  const textColor = styles.getPropertyValue("--text-color").trim() || "#212529";
  const gridColor = styles.getPropertyValue("--box-border").trim() || "#ccc";
  const palette = [
    "#0d6efd",
    "#198754",
    "#ffc107",
    "#dc3545",
    "#6c757d",
    "#6610f2",
    "#fd7e14",
    "#20c997",
  ];

  const projectLabels = config.projectLabels || [];
  const projectData = config.projectData || [];
  const projectCanvas = document.getElementById("projectChart");
  if (projectCanvas && projectLabels.length > 0) {
    destroyChartOnCanvas(projectCanvas);
    new Chart(projectCanvas, {
      type: "doughnut",
      data: {
        labels: projectLabels,
        datasets: [{ data: projectData, backgroundColor: palette }],
      },
      options: {
        plugins: { legend: { labels: { color: textColor } } },
      },
    });
  }

  const completionCanvas = document.getElementById("completionChart");
  if (completionCanvas) {
    destroyChartOnCanvas(completionCanvas);
    new Chart(completionCanvas, {
      type: "bar",
      data: {
        labels: config.completionLabels || [],
        datasets: [
          {
            label: "Completed",
            data: config.completionData || [],
            backgroundColor: "#0d6efd",
          },
        ],
      },
      options: {
        scales: {
          x: { ticks: { color: textColor }, grid: { color: gridColor } },
          y: {
            beginAtZero: true,
            ticks: { stepSize: 1, color: textColor },
            grid: { color: gridColor },
          },
        },
        plugins: { legend: { display: false } },
      },
    });
  }
}

export function initDashboardPage() {
  initTheme();
  attachThemeToggle();
  initDashboardCharts();
}
