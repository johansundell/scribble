// The web app's keyboard shortcuts (#52), as close to the desktop menu bar as
// browsers allow, and the overlay that lists them. Each shortcut clicks the
// link or button marked data-shortcut, so leaving unsaved changes still asks
// first (editor.js). Without it the page works the same, without shortcuts.

var MAC = /Mac|iPhone|iPad/.test(navigator.platform);

// shortcutEl is the element marked data-shortcut="name", if it is shown.
function shortcutEl(name) {
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

// press clicks the named element, and reports whether there was one.
function press(name) {
	var el = shortcutEl(name);
	if (el) el.click();
	return !!el;
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
		if (key === "s" && (el = shortcutEl("save"))) {
			// Never the browser's "Save page", even while a save is running.
			if (!el.disabled) el.form.requestSubmit(el);
			return true;
		}
		if (key === "e") return press("view") || press("edit");
		return false;
	}
	// Ctrl+N opens a browser window. Option+N on macOS is a dead key, which
	// types accents (ñ) in a text field.
	if (e.altKey && !mod && (key === "n" || (e.key === "Dead" && e.code === "KeyN" && !typing(e.target)))) {
		return press("new");
	}
	// Option+T on macOS types †, so outside a text field it goes by the
	// key's place.
	if (e.altKey && !mod && (key === "t" || (e.code === "KeyT" && !typing(e.target)))) {
		return press("theme");
	}
	if (mod || e.altKey) return false;
	if (e.key === "Escape" && e.target === shortcutEl("search")) {
		// Leaves the search box, keeping the filter.
		e.target.blur();
		return true;
	}
	if (typing(e.target)) return false;
	switch (e.key) {
		case "n":
			return press("new");
		case "t":
			return press("theme");
		case "/":
			el = shortcutEl("search");
			if (!el) return false;
			el.focus();
			el.select();
			return true;
		case "F2":
			return press("rename");
		case "?":
			openOverlay();
			return !!overlay();
	}
	return false;
}

document.addEventListener("keydown", function (e) {
	if (e.defaultPrevented || e.isComposing) return;
	// An open overlay or import dialog keeps the keys; Esc closes it.
	if (document.querySelector("dialog[open]")) return;
	if (run(e)) e.preventDefault();
});

document.addEventListener("click", function (e) {
	if (e.target.closest("[data-shortcuts-open]")) {
		openOverlay();
		return;
	}
	var imp = document.getElementById("import");
	if (imp && e.target.closest("[data-import-open]")) {
		imp.showModal();
		return;
	}
	// A tap outside the overlay, the import dialog or the note link picker
	// (#149) closes it.
	if (e.target === overlay() || e.target === imp || e.target.id === "note-link-dialog") e.target.close();
});

function setup() {
	if (MAC) {
		document.querySelectorAll("#shortcuts kbd[data-mod]").forEach(function (k) {
			k.textContent = "⌘";
		});
		document.querySelectorAll("#shortcuts kbd[data-alt]").forEach(function (k) {
			k.textContent = "⌥";
		});
	}
	document.querySelectorAll("[data-shortcuts-open], [data-import-open]").forEach(function (b) {
		b.hidden = false;
	});
}

setup();
