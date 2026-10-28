"use strict";
window.RunCradleTheme = (() => {
  const key = "runcradle.appearance.v1";
  function mount(toggleID, paletteID) {
    let preference = { palette: "blue", dark: false };
    try {
      const saved = JSON.parse(localStorage.getItem(key));
      if (saved && ["blue", "teal"].includes(saved.palette))
        preference = { palette: saved.palette, dark: saved.dark === true };
    } catch {
      /* Standalone exports may have no storage permission. */
    }
    const toggle = document.getElementById(toggleID);
    const palette = document.getElementById(paletteID);
    function apply() {
      document.body.dataset.palette = preference.palette;
      document.body.classList.toggle("dark", preference.dark);
      document.body.classList.toggle("light", !preference.dark);
      palette.value = preference.palette;
      toggle.textContent = preference.dark ? "浅色" : "深暗";
      toggle.setAttribute(
        "aria-label",
        preference.dark ? "切换浅色主题" : "切换深暗主题",
      );
      toggle.setAttribute("aria-pressed", String(preference.dark));
      try {
        localStorage.setItem(key, JSON.stringify(preference));
      } catch {
        /* Optional preference only. */
      }
    }
    toggle.addEventListener("click", () => {
      preference.dark = !preference.dark;
      apply();
    });
    palette.addEventListener("change", () => {
      preference.palette = palette.value === "teal" ? "teal" : "blue";
      apply();
    });
    apply();
  }
  return { mount };
})();
