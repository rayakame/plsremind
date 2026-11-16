// Runs before the first paint so that a dark page never flashes light.
(function () {
  var theme = null;
  try {
    theme = localStorage.getItem("plsremind.theme");
  } catch (error) {
    theme = null;
  }
  if (theme !== "light" && theme !== "dark") {
    theme = matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  document.documentElement.dataset.theme = theme;
})();
