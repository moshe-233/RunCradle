"use strict";
window.RunCradleTheme = (() => {
  const key = "runcradle.appearance.v2";
  const themes = ["blue-light", "teal-light", "graphite-amber"];
  function readPreference(name) {
    try {
      return JSON.parse(localStorage.getItem(name));
    } catch {
      return null;
    }
  }
  function mount(selectID) {
    let theme = themes[0];
    const saved = readPreference(key);
    if (themes.includes(saved?.theme)) theme = saved.theme;
    else {
      const legacy = readPreference("runcradle.appearance.v1");
      if (legacy && ["blue", "teal"].includes(legacy.palette))
        theme =
          legacy.dark === true ? "graphite-amber" : legacy.palette + "-light";
    }
    const select = document.getElementById(selectID);
    function apply() {
      // Theme foreground/background must switch together, including shared SVG.
      document.body.classList.add("theme-changing");
      document.body.dataset.theme = theme;
      document.body.classList.toggle("dark", theme === "graphite-amber");
      document.body.classList.toggle("light", theme !== "graphite-amber");
      select.value = theme;
      // Resolve the new theme with transitions disabled before restoring motion.
      void document.body.offsetWidth;
      document.body.classList.remove("theme-changing");
      try {
        localStorage.setItem(key, JSON.stringify({ theme }));
      } catch {
        /* Optional preference only. */
      }
    }
    select.addEventListener("change", () => {
      theme = themes.includes(select.value) ? select.value : themes[0];
      apply();
    });
    apply();
    return () => {
      theme = themes[(themes.indexOf(theme) + 1) % themes.length];
      apply();
    };
  }
  return { mount };
})();
