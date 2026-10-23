// claim-form.js — Keeps an unchecked component's inputs out of the way on
// /claim. Every element tagged data-claim-component="<c>" (the component
// fieldset and the per-stage <stage>_<c>_env textareas) is disabled while
// the checkbox data-claim-toggle="<c>" is unchecked. Disabled controls skip
// browser constraint validation (e.g. a leftover port below 1024) and are
// not submitted; the server ignores unchecked components anyway. The DOM
// keeps their values, so re-checking the box brings them back. The server
// renders the same initial state, so this only handles live toggling.
(function () {
  "use strict";

  function sync(toggle) {
    const name = toggle.dataset.claimToggle;
    document
      .querySelectorAll('[data-claim-component="' + name + '"]')
      .forEach((el) => {
        el.disabled = !toggle.checked;
      });
  }

  function init() {
    document.querySelectorAll("[data-claim-toggle]").forEach((toggle) => {
      sync(toggle);
      toggle.addEventListener("change", () => sync(toggle));
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
