// The web app's Markdown editor (#53): GitHub's toolbar and smart paste, the
// Write / Preview tabs, the formatting shortcuts and lists that continue on
// Enter. Without it the editor is the plain textarea from #50.
import "./vendor/markdown-toolbar-element/markdown-toolbar-element.js";
import { subscribe } from "./vendor/paste-markdown/paste-markdown.js";

var LOADING = "Loading preview…";
var UNAVAILABLE = "Preview unavailable";

// The formatting shortcuts, Ctrl (Cmd on macOS) plus the key. Ctrl+E is not
// inline code: it toggles view/edit, as in the desktop app (#52).
var FORMAT_KEYS = { b: "md-bold", i: "md-italic", k: "md-link" };

// A list item: indent, marker ("-", "*" or "1."), an optional task box, text.
var LIST_ITEM = /^(\s*)([-*]|(\d+)\.) (\[[ xX]\] )?(.*)$/;

// previewRun counts preview requests, so an answer to an older one is
// dropped.
var previewRun = 0;

// setup enhances the editor on the page once. editor.js asks again after it
// swaps in a new one.
function setup() {
	var form = document.querySelector("form.editor");
	if (!form || form.dataset.markdown) return;
	form.dataset.markdown = "true";
	form.querySelector("[data-md-bar]").hidden = false;
	subscribe(form.querySelector("#note-text"));
}

function editorText(form) {
	return form.querySelector("#note-text");
}

function showTab(form, tab) {
	var text = editorText(form);
	var preview = form.querySelector("#note-preview");
	var toPreview = tab === "preview";
	if (toPreview === !preview.hidden) return;
	form.querySelectorAll("[data-tab]").forEach(function (b) {
		var on = b.dataset.tab === tab;
		b.setAttribute("aria-selected", on ? "true" : "false");
		b.tabIndex = on ? 0 : -1;
	});
	form.querySelector("markdown-toolbar").hidden = toPreview;
	text.hidden = toPreview;
	preview.hidden = !toPreview;
	if (!toPreview) {
		previewRun++;
		// A hidden textarea keeps its selection.
		text.focus();
		return;
	}
	form.querySelector("#tab-preview").focus();
	loadPreview(form, text.value, preview);
}

function loadPreview(form, body, preview) {
	var run = ++previewRun;
	preview.classList.add("muted");
	preview.textContent = LOADING;
	var failed = function () {
		if (run !== previewRun) return;
		preview.textContent = UNAVAILABLE;
	};
	fetch("/app/preview", {
		method: "POST",
		body: new URLSearchParams({ body: body }),
		credentials: "same-origin",
	}).then(function (res) {
		// An ended session is sent to the login page.
		if (!res.ok || res.redirected) return failed();
		return res.text().then(function (html) {
			if (run !== previewRun) return;
			preview.classList.remove("muted");
			// Rendered by the server, which escapes raw HTML as in the view.
			preview.innerHTML = html;
		});
	}).catch(failed);
}

// continueList handles Enter at the end of a list item: it starts the next
// item, or ends the list when the item is empty. It reports whether it did.
function continueList(text) {
	var at = text.selectionStart;
	if (at !== text.selectionEnd) return false;
	var start = text.value.lastIndexOf("\n", at - 1) + 1;
	var end = text.value.indexOf("\n", at);
	if (end < 0) end = text.value.length;
	if (at !== end) return false;
	var m = LIST_ITEM.exec(text.value.slice(start, at));
	if (!m) return false;
	if (m[5] === "") {
		// An empty item ends the list: its marker goes.
		text.setSelectionRange(start, at);
		insert(text, "");
		return true;
	}
	var marker = m[3] ? Number(m[3]) + 1 + "." : m[2];
	insert(text, "\n" + m[1] + marker + " " + (m[4] ? "[ ] " : ""));
	return true;
}

// insert types s over the selection, so it can be undone and editor.js sees
// the change.
function insert(text, s) {
	var ok = false;
	try {
		ok = s === "" ? document.execCommand("delete") : document.execCommand("insertText", false, s);
	} catch (e) {
		ok = false;
	}
	if (!ok) {
		text.setRangeText(s, text.selectionStart, text.selectionEnd, "end");
		text.dispatchEvent(new Event("input", { bubbles: true }));
	}
}

document.addEventListener("click", function (e) {
	var tab = e.target.closest("form.editor [data-tab]");
	if (tab) showTab(tab.form, tab.dataset.tab);
});

document.addEventListener("keydown", function (e) {
	var form = e.target.closest && e.target.closest("form.editor");
	if (!form || !form.dataset.markdown || e.isComposing) return;
	var mod = e.ctrlKey || e.metaKey;
	var key = e.key.toLowerCase();
	if (mod && e.shiftKey && !e.altKey && key === "p") {
		e.preventDefault();
		showTab(form, editorText(form).hidden ? "write" : "preview");
		return;
	}
	if (e.target.closest("[role=tablist]") && (e.key === "ArrowLeft" || e.key === "ArrowRight")) {
		e.preventDefault();
		showTab(form, editorText(form).hidden ? "write" : "preview");
		return;
	}
	if (e.target !== editorText(form)) return;
	if (mod && !e.shiftKey && !e.altKey && FORMAT_KEYS[key]) {
		e.preventDefault();
		form.querySelector(FORMAT_KEYS[key]).click();
		return;
	}
	if (e.key === "Enter" && !mod && !e.shiftKey && !e.altKey && continueList(e.target)) {
		e.preventDefault();
	}
});

document.addEventListener("editor:ready", setup);
setup();
