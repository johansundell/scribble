// The theme switcher (#87): system, light or dark, kept in localStorage.
// app.html's inline script sets data-theme before the page paints; this
// shows the header button, cycles the theme on a click (or t, Alt+T in
// shortcuts.js) and keeps the browser's bars in step. Without JavaScript the
// page follows the system.
(function () {
	"use strict";
	var THEMES = ["system", "light", "dark"];
	var LABELS = { system: "System", light: "Light", dark: "Dark" };
	var ICONS = { system: "💻", light: "☀️", dark: "🌙" };
	// app.css's --surface in each theme: the header's colour.
	var BAR = { light: "#ffffff", dark: "#1e293b" };

	var button = document.querySelector("[data-theme-toggle]");
	if (!button) return;
	var theme = saved();
	apply();
	button.hidden = false;
	button.addEventListener("click", function () {
		theme = THEMES[(THEMES.indexOf(theme) + 1) % THEMES.length];
		try {
			localStorage.setItem("theme", theme);
		} catch (e) {
			// Private windows may refuse; the theme lasts for this page.
		}
		apply();
	});

	function saved() {
		try {
			var t = localStorage.getItem("theme");
			if (THEMES.indexOf(t) >= 0) return t;
		} catch (e) {}
		return "system";
	}

	// apply sets data-theme, the button and theme-color. In system mode each
	// theme-color meta keeps to its own media query, so the bars follow the
	// system as app.css does.
	function apply() {
		var root = document.documentElement;
		if (theme === "system") delete root.dataset.theme;
		else root.dataset.theme = theme;
		document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) {
			m.content = BAR[theme !== "system" ? theme : /dark/.test(m.media) ? "dark" : "light"];
		});
		var label = "Theme: " + LABELS[theme] + " (click to switch)";
		button.textContent = ICONS[theme];
		button.setAttribute("aria-label", label);
		button.title = label;
	}
})();
