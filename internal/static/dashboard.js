// dashboard.js — view toggle, table sort, column toggle, and FE-side
// activity-score recomputation for the landing page. Vanilla JS, no
// framework. State persists in localStorage so a refresh keeps your
// layout. All numeric data is on the row's data-* attributes (set by
// the templ template) — we never re-fetch from the server for sort or
// threshold changes.
(function () {
  "use strict";

  const LS_KEY = "platform-ui.dashboard.v1";
  const DEFAULT_STATE = {
    view: "cards", // "cards" | "table"
    sort: { key: "slug", dir: "asc" },
    hiddenColumns: ["restarts", "5xx", "memory", "last-deploy"],
    thresholds: {
      "active-req24h": 100,
      "quiet-req7d": 100,
      "idle-days": 30,
      "unhealthy-5xx-pct": 5,
      "unhealthy-restarts": 10,
    },
  };

  // ---- state -----------------------------------------------------------

  function loadState() {
    try {
      const raw = localStorage.getItem(LS_KEY);
      if (!raw) return { ...DEFAULT_STATE };
      const parsed = JSON.parse(raw);
      // merge — new defaults win over missing keys
      return {
        ...DEFAULT_STATE,
        ...parsed,
        thresholds: { ...DEFAULT_STATE.thresholds, ...(parsed.thresholds || {}) },
      };
    } catch (e) {
      console.warn("dashboard: localStorage parse failed; using defaults", e);
      return { ...DEFAULT_STATE };
    }
  }

  function saveState(s) {
    try {
      localStorage.setItem(LS_KEY, JSON.stringify(s));
    } catch (e) {
      // quota? ignore.
    }
  }

  let state = loadState();

  // ---- DOM -------------------------------------------------------------

  const dashboard = document.getElementById("dashboard");
  if (!dashboard) return;

  const viewButtons = document.querySelectorAll(".view-toggle .view-btn");
  const columnToggles = document.querySelectorAll(".column-toggle input[data-column]");
  const thresholdInputs = document.querySelectorAll("[data-threshold]");
  const resetThresholdsBtn = document.getElementById("reset-thresholds");
  const table = document.getElementById("tenants-table");

  // ---- view toggle -----------------------------------------------------

  function applyView() {
    dashboard.dataset.view = state.view;
    viewButtons.forEach((b) =>
      b.classList.toggle("is-active", b.dataset.view === state.view)
    );
  }

  viewButtons.forEach((b) => {
    b.addEventListener("click", () => {
      state.view = b.dataset.view;
      applyView();
      saveState(state);
    });
  });

  // ---- column toggle ---------------------------------------------------

  function applyColumns() {
    const hidden = new Set(state.hiddenColumns);

    document
      .querySelectorAll(
        ".tenants-table th[data-column], .tenants-table td[data-column]"
      )
      .forEach((el) => {
        el.hidden = hidden.has(el.dataset.column);
      });

    columnToggles.forEach((cb) => {
      cb.checked = !hidden.has(cb.dataset.column);
    });
  }

  columnToggles.forEach((cb) => {
    cb.addEventListener("change", () => {
      const col = cb.dataset.column;
      const set = new Set(state.hiddenColumns);
      if (cb.checked) set.delete(col);
      else set.add(col);
      state.hiddenColumns = [...set];
      applyColumns();
      saveState(state);
    });
  });

  // ---- sorting ---------------------------------------------------------

  function compareNum(a, b, dir) {
    // -1 sentinel = "no data", always sort to the bottom regardless of dir
    if (a === -1 && b !== -1) return 1;
    if (b === -1 && a !== -1) return -1;
    return dir === "asc" ? a - b : b - a;
  }

  function compareText(a, b, dir) {
    const r = a.localeCompare(b);
    return dir === "asc" ? r : -r;
  }

  function getSortValue(row, key) {
    if (key === "slug") return row.dataset.slug || "";
    if (key === "drift-total")
      return parseInt(row.dataset.driftTotal || "0", 10);
    const attr = "data-" + key;
    const raw = row.getAttribute(attr);
    return raw == null ? -1 : parseFloat(raw);
  }

  function applySort() {
    if (!table) return;
    const tbody = table.querySelector("tbody");
    const rows = [...tbody.querySelectorAll("tr.tenant-row")];

    const { key, dir } = state.sort;
    const ths = table.querySelectorAll("th[data-sort]");
    ths.forEach((th) => {
      th.classList.toggle("sort-asc", th.dataset.sortKey === key && dir === "asc");
      th.classList.toggle("sort-desc", th.dataset.sortKey === key && dir === "desc");
    });

    // also update the slug header (no data-sort-key)
    const slugTh = table.querySelector('th[data-column="slug"]');
    if (slugTh) {
      slugTh.classList.toggle("sort-asc", key === "slug" && dir === "asc");
      slugTh.classList.toggle("sort-desc", key === "slug" && dir === "desc");
    }

    rows.sort((a, b) => {
      const va = getSortValue(a, key);
      const vb = getSortValue(b, key);
      if (typeof va === "string" || typeof vb === "string") {
        return compareText(String(va), String(vb), dir);
      }
      return compareNum(va, vb, dir);
    });

    rows.forEach((r) => tbody.appendChild(r));
  }

  if (table) {
    const headers = table.querySelectorAll("th[data-sort], th[data-column='slug']");
    headers.forEach((th) => {
      th.style.cursor = "pointer";
      th.addEventListener("click", () => {
        const key = th.dataset.sortKey || (th.dataset.column === "slug" ? "slug" : null);
        if (!key) return;
        const dir =
          state.sort.key === key && state.sort.dir === "asc" ? "desc" : "asc";
        state.sort = { key, dir };
        applySort();
        saveState(state);
      });
    });
  }

  // ---- activity score recomputation ------------------------------------

  // Reapply the classification based on the current thresholds. The
  // server already wrote a default label into data-prod-activity (and
  // -staging-activity) so the page is useful without JS; this just
  // overrides when the user has tweaked the thresholds.
  function recomputeActivity() {
    const t = state.thresholds;

    document.querySelectorAll("[data-slug]").forEach((row) => {
      ["prod", "staging"].forEach((stage) => {
        const label = classifyStage(row, stage, t);
        row.dataset[stage + "Activity"] = label;
        // also update visible badges that mirror this stage
        row
          .querySelectorAll(
            `.badge--activity[data-stage="${stage}"]`
          )
          .forEach((b) => {
            // strip any existing activity--* class
            b.classList.forEach((c) => {
              if (c.startsWith("activity--")) b.classList.remove(c);
            });
            b.classList.add("activity--" + label);
            b.dataset.activity = label;
            b.textContent = stage + ": " + wordFor(label);
          });
      });
    });
  }

  function classifyStage(row, stage, t) {
    const req24h = parseFloat(row.getAttribute("data-" + stage + "-req24h"));
    if (Number.isNaN(req24h) || req24h === -1) {
      // staging-specific raw fields aren't all populated; fall back to
      // server-provided label so we don't regress when thresholds are
      // unchanged.
      return row.dataset[stage + "Activity"] || "unknown";
    }

    // We only have full per-stage metric coverage for prod for now; staging
    // re-uses prod's secondary signals (5xx, restarts) heuristically.
    const fivexx =
      stage === "prod"
        ? parseFloat(row.getAttribute("data-prod-5xx") || "0")
        : 0;
    const restarts =
      stage === "prod"
        ? parseFloat(row.getAttribute("data-prod-restarts24h") || "0")
        : 0;

    if (fivexx * 100 > t["unhealthy-5xx-pct"] || restarts > t["unhealthy-restarts"]) {
      return "unhealthy";
    }

    const req7d =
      stage === "prod"
        ? parseFloat(row.getAttribute("data-prod-req7d") || "-1")
        : req24h * 7; // crude fallback

    const lastTs =
      stage === "prod"
        ? parseFloat(row.getAttribute("data-prod-last-request") || "-1")
        : -1;
    const lastDays =
      lastTs > 0 ? (Date.now() / 1000 - lastTs) / 86400 : Infinity;

    if (req24h >= t["active-req24h"]) return "active";
    if (req7d >= t["quiet-req7d"]) return "quiet";
    if (lastDays <= t["idle-days"]) return "idle";
    return "dormant";
  }

  function wordFor(label) {
    return (
      {
        active: "aktiv",
        quiet: "ruhig",
        idle: "idle",
        dormant: "verwaist",
        unhealthy: "ungesund",
        unknown: "—",
      }[label] || label
    );
  }

  // ---- thresholds UI ---------------------------------------------------

  function applyThresholds() {
    thresholdInputs.forEach((inp) => {
      const key = inp.dataset.threshold;
      if (state.thresholds[key] != null) inp.value = state.thresholds[key];
    });
    recomputeActivity();
  }

  thresholdInputs.forEach((inp) => {
    inp.addEventListener("change", () => {
      const key = inp.dataset.threshold;
      const v = parseFloat(inp.value);
      if (!Number.isNaN(v)) {
        state.thresholds[key] = v;
        applyThresholds();
        saveState(state);
      }
    });
  });

  if (resetThresholdsBtn) {
    resetThresholdsBtn.addEventListener("click", () => {
      state.thresholds = { ...DEFAULT_STATE.thresholds };
      applyThresholds();
      saveState(state);
    });
  }

  // ---- init ------------------------------------------------------------

  applyView();
  applyColumns();
  applyThresholds();
  applySort();
})();
