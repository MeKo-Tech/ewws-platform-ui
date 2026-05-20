// detail-chart.js — Window toggle (7d / 30d) and hover tooltip for the
// per-stage traffic charts on /app/<slug>. The server pre-renders both
// windows as data-points-7 / data-points-30 (SVG polyline) plus the raw
// hourly counts as JSON in data-hours-7 / data-hours-30. We just swap
// the polyline points + recompute hover positions client-side.
(function () {
  "use strict";

  const LS_KEY = "platform-ui.detail-chart.window";
  const validWindows = new Set(["7", "30"]);

  let currentWindow = (() => {
    try {
      const w = localStorage.getItem(LS_KEY);
      return validWindows.has(w) ? w : "7";
    } catch (_) {
      return "7";
    }
  })();

  const root = document.getElementById("traffic-charts");
  if (!root) return;

  const buttons = root.querySelectorAll(".window-btn");
  const cards = root.querySelectorAll(".chart-card");

  function applyWindow(w) {
    if (!validWindows.has(w)) w = "7";
    currentWindow = w;

    buttons.forEach((b) => b.classList.toggle("is-active", b.dataset.window === w));

    cards.forEach((card) => {
      const svg = card.querySelector("svg.chart-svg");
      if (!svg) return;
      const poly = svg.querySelector(".chart-line");
      const pts = svg.getAttribute("data-points-" + w);
      if (poly) {
        if (pts) {
          poly.setAttribute("points", pts);
          poly.removeAttribute("hidden");
        } else {
          poly.removeAttribute("points");
        }
      }

      // Update aggregate stat (total + peak)
      const sumEl = card.querySelector("[data-stat-" + w + "]");
      if (sumEl) {
        sumEl.textContent = formatCount(parseInt(sumEl.getAttribute("data-stat-" + w), 10));
      }

      const peakEl = card.querySelector("[data-peak-" + w + "]");
      if (peakEl) {
        peakEl.textContent = formatCount(parseInt(peakEl.getAttribute("data-peak-" + w), 10));
      }

      // Hide hover overlay; the user will re-trigger it.
      const hover = svg.querySelector(".chart-hover");
      if (hover) hover.setAttribute("hidden", "");

      const tooltip = card.querySelector(".chart-tooltip");
      if (tooltip) tooltip.setAttribute("hidden", "");
    });

    try {
      localStorage.setItem(LS_KEY, w);
    } catch (_) {
      // quota — ignore
    }
  }

  function formatCount(n) {
    if (!Number.isFinite(n) || n === 0) return "0";
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
    if (n >= 1_000) return (n / 1_000).toFixed(1) + "k";
    return String(n);
  }

  function formatHourLabel(startIso, idx) {
    if (!startIso) return "Bucket #" + idx;
    const d = new Date(startIso);
    d.setUTCHours(d.getUTCHours() + idx);
    return d.toISOString().slice(0, 13).replace("T", " ") + ":00 UTC";
  }

  function attachHover(card) {
    const svg = card.querySelector("svg.chart-svg");
    if (!svg) return;
    const hover = svg.querySelector(".chart-hover");
    const cursor = svg.querySelector(".chart-cursor");
    const dot = svg.querySelector(".chart-dot");
    const tooltip = card.querySelector(".chart-tooltip");

    if (!hover || !cursor || !dot || !tooltip) return;

    svg.addEventListener("mousemove", (e) => {
      const w = currentWindow;
      const hoursAttr = svg.getAttribute("data-hours-" + w);
      const startIso = svg.getAttribute("data-start-" + w);
      if (!hoursAttr) return;
      let buckets;
      try {
        buckets = JSON.parse(hoursAttr);
      } catch (_) {
        return;
      }
      if (!buckets.length) return;

      const rect = svg.getBoundingClientRect();
      const xPct = (e.clientX - rect.left) / rect.width;
      const idx = Math.max(0, Math.min(buckets.length - 1, Math.round(xPct * (buckets.length - 1))));
      const value = buckets[idx];
      const maxV = Math.max(...buckets);

      const xVB = (idx / (buckets.length - 1)) * 800;
      const yVB = maxV === 0 ? 200 : 200 - (value / maxV) * 190;

      cursor.setAttribute("x1", xVB.toFixed(1));
      cursor.setAttribute("x2", xVB.toFixed(1));
      dot.setAttribute("cx", xVB.toFixed(1));
      dot.setAttribute("cy", yVB.toFixed(1));
      hover.removeAttribute("hidden");

      tooltip.removeAttribute("hidden");
      tooltip.textContent = formatCount(value) + " req · " + formatHourLabel(startIso, idx);
      // Position the tooltip in pixel-space anchored to the mouse but
      // clamped to the card frame.
      const cardRect = card.getBoundingClientRect();
      const left = Math.min(
        Math.max(0, e.clientX - cardRect.left + 12),
        cardRect.width - tooltip.offsetWidth - 4
      );
      tooltip.style.left = left + "px";
      tooltip.style.top = Math.max(0, e.clientY - cardRect.top - tooltip.offsetHeight - 8) + "px";
    });

    svg.addEventListener("mouseleave", () => {
      hover.setAttribute("hidden", "");
      tooltip.setAttribute("hidden", "");
    });
  }

  buttons.forEach((b) =>
    b.addEventListener("click", () => applyWindow(b.dataset.window))
  );

  cards.forEach(attachHover);

  applyWindow(currentWindow);
})();
