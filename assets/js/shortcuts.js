// The web app's keyboard shortcuts (#52), as close to the desktop menu bar as
// browsers allow, and the overlay that lists them. Each shortcut clicks the
// link or button marked data-shortcut, so leaving unsaved changes still asks
// first (editor.js). Without it the page works the same, without shortcuts.

var MAC = /Mac|iPhone|iPad/.test(navigator.platform);

function target(name) {
	var el = document.querySelector('[data-shortcut="' + name + '"]');
	return el && !el.hidden ? el : null;
}

// typing says the focus is in a text field, where single keys are just typed.
function typing(el) {
	return el.isContentEditable || (el.matches && el.matches("input, textarea, select"));
}

function overlay() {
	return document.getElementById("shortcuts");
}

function openOverlay() {
	var d = overlay();
	if (d && !d.open) d.showModal();
}

// run does what the key asks for, and reports whether it did.
function run(e) {
	var mod = e.ctrlKey || e.metaKey;
	var key = e.key.toLowerCase();
	var el;
	if (mod && !e.shiftKey && !e.altKey) {
		if (key === "s" && (el = target("save"))) {
			// Never the browser's "Save page", even while a save is running.
			if (!el.disabled) el.form.requestSubmit(el);
			return true;
		}
		if (key === "e" && (el = target("view") || target("edit"))) {
			el.click();
			return true;
		}
		return false;
	}
	// Ctrl+N opens a browser window. Option+N on macOS is a dead key.
	if (e.altKey && !mod && (key === "n" || (e.key === "Dead" && e.code === "KeyN"))) {
		el = target("new");
		if (el) el.click();
		return !!el;
	}
	if (mod || e.altKey) return false;
	if (e.key === "Escape" && e.target === target("search")) {
		// Leaves the search box, keeping the filter.
		e.target.blur();
		return true;
	}
	if (typing(e.target)) return false;
	switch (e.key) {
		case "n":
			el = target("new");
			if (el) el.click();
			return !!el;
		case "/":
			el = target("search");
			if (!el) return false;
			el.focus();
			el.select();
			return true;
		case "F2":
			el = target("rename");
			if (el) el.click();
			return !!el;
		case "?":
			openOverlay();
			return !!overlay();
	}
	return false;
}

document.addEventListener("keydown", function (e) {
	if (e.defaultPrevented || e.isComposing) return;
	// The open overlay keeps the keys; Esc closes it.
	var d = overlay();
	if (d && d.open) return;
	if (run(e)) e.preventDefault();
});

document.addEventListener("click", function (e) {
	if (e.target.closest("[data-shortcuts-open]")) {
		openOverlay();
		return;
	}
	// A tap outside the overlay closes it.
	var d = overlay();
	if (d && e.target === d) d.close();
});

function setup() {
	if (MAC) {
		document.querySelectorAll("#shortcuts kbd[data-mod]").forEach(function (k) {
			k.textContent = "⌘";
		});
	}
	document.querySelectorAll("[data-shortcuts-open]").forEach(function (b) {
		b.hidden = false;
	});
}

setup();
