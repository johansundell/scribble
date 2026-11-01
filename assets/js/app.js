// The web app's Markdown editor (#53): GitHub's toolbar and smart paste, the
// Write / Preview tabs, the formatting shortcuts and lists that continue on
// Enter. Without it the editor is the plain textarea from #50.
import "./vendor/markdown-toolbar-element/markdown-toolbar-element.js";
import { installLink, installTable } from "./vendor/paste-markdown/paste-markdown.js";

var LOADING = "Loading preview…";
var UNAVAILABLE = "Preview unavailable";

// The formatting shortcuts, Ctrl (Cmd on macOS) plus the key. Ctrl+E is not
// inline code: it toggles view/edit, as in the desktop app (#52).
var FORMAT_KEYS = { b: "md-bold", i: "md-italic", k: "md-link" };

// A list item: indent, marker ("-", "*" or "1."), an optional task box, text.
var LIST_ITEM = /^(\s*)([-*]|(\d+)\.) (\[[ xX]\] )?(.*)$/;
// A thematic break such as "* * *" or "- - -", which is not a list item.
var RULE = /^\s*([-*])(\s*\1){2,}\s*$/;

var MAC = /Mac|iPhone|iPad/.test(navigator.platform);

// On phones the textarea grows with the text (#65). app.css does it with
// field-sizing where the browser has it; grow does it elsewhere. PHONE is
// app.css's phone breakpoint.
var PHONE = window.matchMedia("(width < 700px)");
var FIELD_SIZING = window.CSS && CSS.supports("field-sizing", "content");

function grow(text) {
	if (FIELD_SIZING || !PHONE.matches) return;
	text.style.height = "auto";
	text.style.height = text.scrollHeight + 2 + "px";
}

// previewRun counts preview requests, so an answer to an older one is dropped.
var previewRun = 0;

// setup enhances the editor on the page once. editor.js asks again after it
// swaps in a new one.
function setup() {
	var form = document.querySelector("form.editor");
	if (!form || form.dataset.markdown) return;
	form.dataset.markdown = "true";
	form.querySelector("[data-md-bar]").hidden = false;
	if (MAC) {
		form.querySelectorAll(".md-button[title]").forEach(function (b) {
			b.title = b.title.replace("Ctrl+Shift+", "⇧⌘").replace("Ctrl+", "⌘");
		});
	}
	var text = editorText(form);
	// Only the two pastes from #53, as in the desktop app (#54): a URL over
	// selected text, and spreadsheet cells.
	installLink(text);
	installTable(text);
	text.addEventListener("input", function () {
		grow(text);
	});
	grow(text);
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
	fetch("/preview", {
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
	var line = text.value.slice(start, at);
	var m = LIST_ITEM.exec(line);
	if (!m || RULE.test(line)) return false;
	if (m[5].trim() === "") {
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

// Link to note (#149): a picker over the other notes' titles that puts a
// [[link]] to the chosen one in place of the selection, as one undo step.
// picker holds the open picker's state: the textarea it links into, the
// selection there, the titles, those that match the search and the chosen
// one. run counts openings, so a late fetch for an older one is dropped.
var picker = { text: null, start: 0, end: 0, titles: [], shown: [], active: -1, run: 0 };

function linkDialog() {
	return document.getElementById("note-link-dialog");
}

// listTitles is the titles in doc's note list, less the note at path skip
// and any a [[link]] cannot hold, as in the desktop app (#143).
function listTitles(doc, skip) {
	var out = [];
	doc.querySelectorAll(".note-items a").forEach(function (a) {
		if (new URL(a.getAttribute("href"), location.href).pathname === skip) return;
		// The title is the link's own text, without the pin badge.
		var title = Array.from(a.childNodes)
			.filter(function (n) { return n.nodeType === Node.TEXT_NODE; })
			.map(function (n) { return n.nodeValue; })
			.join("")
			.trim();
		if (title && !/[\[\]\n]/.test(title) && out.indexOf(title) < 0) out.push(title);
	});
	return out;
}

function openPicker(form) {
	var d = linkDialog();
	if (!d || d.open) return;
	var text = editorText(form);
	var run = ++picker.run;
	// The note being edited; a new note has none. Not form.action: that is
	// the button named "action".
	var skip = new URL(form.getAttribute("action"), location.href).pathname.replace(/\/edit$/, "");
	picker.text = text;
	picker.start = text.selectionStart;
	picker.end = text.selectionEnd;
	picker.titles = listTitles(document, skip);
	var q = document.getElementById("note-link-search");
	q.value = text.value.slice(picker.start, picker.end).trim();
	d.returnValue = "";
	d.showModal();
	q.focus();
	filterPicker();
	// A filtered list doesn't hold every note: the unfiltered one does.
	var f = document.querySelector("form.list-filter");
	var filtered = f && ((f.elements.q && f.elements.q.value.trim() !== "") || (f.elements.project && f.elements.project.value !== ""));
	if (!filtered && picker.titles.length) return;
	fetch("/", { credentials: "same-origin" })
		.then(function (res) {
			// An ended session is sent to the login page.
			if (!res.ok || res.redirected) return;
			return res.text().then(function (html) {
				if (run !== picker.run) return;
				picker.titles = listTitles(new DOMParser().parseFromString(html, "text/html"), skip);
				filterPicker();
			});
		})
		.catch(function () {});
}

// filterPicker lists the titles holding the search, ignoring case, the
// first one chosen.
function filterPicker() {
	var q = document.getElementById("note-link-search").value.trim().toLowerCase();
	var list = document.getElementById("note-link-list");
	picker.shown = picker.titles.filter(function (t) {
		return t.toLowerCase().indexOf(q) >= 0;
	});
	list.replaceChildren.apply(
		list,
		picker.shown.map(function (t, i) {
			var li = document.createElement("li");
			li.id = "note-link-" + i;
			li.setAttribute("role", "option");
			li.textContent = t;
			return li;
		})
	);
	linkDialog().querySelector("[data-note-link-empty]").hidden = picker.shown.length > 0;
	linkDialog().querySelector("[data-note-link-ok]").disabled = picker.shown.length === 0;
	choose(picker.shown.length ? 0 : -1);
}

// choose marks the title at index i as the one Enter or Link links to.
function choose(i) {
	picker.active = i;
	var q = document.getElementById("note-link-search");
	document.querySelectorAll("#note-link-list [role=option]").forEach(function (li, n) {
		li.setAttribute("aria-selected", n === i ? "true" : "false");
		if (n === i) li.scrollIntoView({ block: "nearest" });
	});
	if (i >= 0) q.setAttribute("aria-activedescendant", "note-link-" + i);
	else q.removeAttribute("aria-activedescendant");
}

// pickerKey handles the keys in the picker's search: Up and Down choose,
// Enter links, Esc closes. Esc is handled here because a search field with
// text in it would otherwise take the first press to clear itself.
function pickerKey(e) {
	var n = picker.shown.length;
	if ((e.key === "ArrowDown" || e.key === "ArrowUp") && n) {
		e.preventDefault();
		choose(Math.min(n - 1, Math.max(0, picker.active + (e.key === "ArrowDown" ? 1 : -1))));
	} else if (e.key === "Enter") {
		// Not the form's default button, which is Cancel.
		e.preventDefault();
		if (picker.active >= 0) linkDialog().close("link");
	} else if (e.key === "Escape") {
		e.preventDefault();
		linkDialog().close("");
	}
}

// closePicker puts the focus and selection back in the editor and, when a
// title was chosen, its [[link]] in place of the selection.
function closePicker() {
	var text = picker.text;
	if (!text) return;
	picker.text = null;
	picker.run++;
	// nav.js may have swapped in another page while the picker was open.
	if (!text.isConnected) return;
	var title = linkDialog().returnValue === "link" ? picker.shown[picker.active] : "";
	text.focus();
	text.setSelectionRange(picker.start, picker.end);
	if (title) insert(text, "[[" + title + "]]");
}

if (linkDialog()) linkDialog().addEventListener("close", closePicker);

document.addEventListener("input", function (e) {
	if (e.target.id === "note-link-search") filterPicker();
});

document.addEventListener("click", function (e) {
	var tab = e.target.closest("form.editor [data-tab]");
	if (tab) showTab(tab.form, tab.dataset.tab);
	var link = e.target.closest("form.editor [data-note-link]");
	if (link) openPicker(link.form);
	var option = e.target.closest("#note-link-list [role=option]");
	if (option) {
		choose(Array.prototype.indexOf.call(option.parentNode.children, option));
		linkDialog().close("link");
	}
});

document.addEventListener("keydown", function (e) {
	if (e.target.id === "note-link-search" && !e.isComposing) {
		pickerKey(e);
		return;
	}
	var form = e.target.closest && e.target.closest("form.editor");
	if (!form || !form.dataset.markdown || e.isComposing) return;
	var mod = e.ctrlKey || e.metaKey;
	var key = e.key.toLowerCase();
	// With two tabs, either arrow key in the tab list goes to the other one.
	var arrow = e.target.closest("[role=tablist]") && (e.key === "ArrowLeft" || e.key === "ArrowRight");
	if ((mod && e.shiftKey && !e.altKey && key === "p") || arrow) {
		e.preventDefault();
		showTab(form, editorText(form).hidden ? "write" : "preview");
		return;
	}
	if (e.target !== editorText(form)) return;
	if (mod && e.shiftKey && !e.altKey && key === "k") {
		e.preventDefault();
		openPicker(form);
		return;
	}
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
