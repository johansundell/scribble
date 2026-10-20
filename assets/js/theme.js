// The theme switcher (#87): system, light or dark, kept in localStorage.
// app.html's inline script sets data-theme before the page paints; this
// shows the header button, cycles the theme on a click (or t, Alt+T in
// shortcuts.js) and keeps the browser's bars in step. Without JavaScript the
// page follows the system.
(function () {
	"use strict";
	var THEMES = {
		system: { label: "System", icon: "💻" },
		light: { label: "Light", icon: "☀️" },
		dark: { label: "Dark", icon: "🌙" },
	};
	var ORDER = ["system", "light", "dark"];

	var button = document.querySelector("[data-theme-toggle]");
	if (!button) return;
	var bars = document.querySelectorAll('meta[name="theme-color"]');
	// Each meta's colour for its own media query, for system mode.
	bars.forEach(function (m) {
		m.dataset.system = m.content;
	});
	var theme = savedTheme();
	apply();
	button.hidden = false;
	button.addEventListener("click", function () {
		theme = ORDER[(ORDER.indexOf(theme) + 1) % ORDER.length];
		try {
			localStorage.setItem("theme", theme);
		} catch (e) {
			// Private windows may refuse; the theme lasts for this page.
		}
		apply();
	});

	function savedTheme() {
		try {
			var t = localStorage.getItem("theme");
			if (ORDER.indexOf(t) >= 0) return t;
		} catch (e) {}
		return "system";
	}

	// apply sets data-theme, the button and theme-color. A pinned theme gives
	// the bars the header's colour (--surface); in system mode each meta keeps
	// to its media query, so the bars follow the system as app.css does.
	function apply() {
		var root = document.documentElement;
		if (theme === "system") delete root.dataset.theme;
		else root.dataset.theme = theme;
		var surface = getComputedStyle(root).getPropertyValue("--surface").trim();
		bars.forEach(function (m) {
			m.content = theme === "system" ? m.dataset.system : surface;
		});
		var label = "Theme: " + THEMES[theme].label + " (click to switch)";
		button.textContent = THEMES[theme].icon;
		button.setAttribute("aria-label", label);
		button.title = label;
	}
})();
