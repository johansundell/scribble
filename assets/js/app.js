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
	// A pasted image is uploaded (#201). It goes first, so the pastes below
	// never see it.
	text.addEventListener("paste", pasteImage);
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
	// The editor names where it previews: an edit link's under its token
	// (#197).
	fetch(form.dataset.preview, {
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
			// copy.js adds the code blocks' Copy buttons.
			document.dispatchEvent(new Event("scribble:preview"));
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

// codeBlock puts the selection in a fenced code block and keeps it
// selected, or, with none, puts an empty block at the cursor with the
// cursor on its blank line (#176), as the desktop app's insertCodeBlock
// does. The fences go on lines of their own. A selection that ends at the
// start of a line leaves that newline out of the block.
function codeBlock(text) {
	var v = text.value;
	var start = text.selectionStart;
	var end = text.selectionEnd;
	if (end > start && v.charAt(end - 1) === "\n") end--;
	var open = (start > 0 && v.charAt(start - 1) !== "\n" ? "\n" : "") + "```\n";
	var closing = "\n```" + (end < v.length && v.charAt(end) !== "\n" ? "\n" : "");
	var sel = v.slice(start, end);
	text.focus();
	text.setSelectionRange(start, end);
	insert(text, open + sel + closing);
	var at = start + open.length;
	text.setSelectionRange(at, at + sel.length);
}

// Images (#201): the Image button, a dropped file and a pasted one all go
// through uploadImage, which holds the image's place at the cursor with
// "![Uploading <name>…]()" until the server answers with its URL.
var IMAGE_UNREACHABLE = "The server can't be reached, so the image wasn't added. Try again.";
var IMAGE_SESSION = "Your session has ended, so the image wasn't added. Log in again, then add it.";

// altText is file's name as an image description: no brackets, backslashes
// or line breaks, which would end it early, and "image" when it has none.
function altText(file) {
	var name = (file.name || "").replace(/[[\]\\\r\n]/g, "").trim();
	return name || "image";
}

function uploadImages(form, files) {
	Array.prototype.forEach.call(files, function (file) {
		uploadImage(form, file);
	});
}

function uploadImage(form, file) {
	var text = editorText(form);
	var alt = altText(file);
	var mark = "![Uploading " + alt + "…]()";
	text.focus();
	insert(text, mark);
	var data = new FormData();
	data.append("file", file);
	fetch(form.dataset.images, { method: "POST", body: data, credentials: "same-origin" })
		.then(function (res) {
			// A post from an ended session is sent to the login.
			if (res.redirected) throw new Error(IMAGE_SESSION);
			return res.json().catch(function () {
				return {};
			}).then(function (body) {
				if (!res.ok || !body.url) {
					throw new Error(body.error || "Adding the image failed (" + res.status + "). Try again.");
				}
				return body.url;
			});
		}, function () {
			throw new Error(IMAGE_UNREACHABLE);
		})
		.then(function (url) {
			replaceMark(text, mark, "![" + alt + "](" + url + ")");
		}, function (err) {
			replaceMark(text, mark, "");
			var shown = form.querySelector(".editor-error");
			shown.textContent = err.message;
			shown.hidden = false;
		});
}

// replaceMark puts s in place of the first mark, keeping the cursor where
// it is: after s when it was at or in the mark. ("preserve" would select s,
// and the next upload would type over it.) A mark the user removed is left
// removed.
function replaceMark(text, mark, s) {
	var at = text.value.indexOf(mark);
	if (at < 0) return;
	var after = at + mark.length;
	var moved = function (p) {
		if (p >= after) return p + s.length - mark.length;
		return p > at ? at + s.length : p;
	};
	var start = moved(text.selectionStart);
	var end = moved(text.selectionEnd);
	text.setRangeText(s, at, after);
	text.setSelectionRange(start, end);
	text.dispatchEvent(new Event("input", { bubbles: true }));
}

// pasteImage uploads pasted files, such as a screenshot. A paste with text
// is left alone: a spreadsheet's cells come with a picture of them too.
function pasteImage(e) {
	var cb = e.clipboardData;
	if (!cb || !cb.files.length || Array.prototype.indexOf.call(cb.types, "text/plain") >= 0) return;
	e.preventDefault();
	e.stopImmediatePropagation();
	uploadImages(e.target.form, cb.files);
}

// hasFiles reports whether a drag carries files.
function hasFiles(e) {
	return !!e.dataTransfer && Array.prototype.indexOf.call(e.dataTransfer.types, "Files") >= 0;
}

// dropTarget is the editor's textarea e is over, or null.
function dropTarget(e) {
	var text = e.target.closest && e.target.closest("form.editor #note-text");
	return text && text.form.dataset.markdown ? text : null;
}

document.addEventListener("dragover", function (e) {
	if (dropTarget(e) && hasFiles(e)) e.preventDefault();
});

document.addEventListener("drop", function (e) {
	var text = dropTarget(e);
	if (!text || !hasFiles(e) || !e.dataTransfer.files.length) return;
	e.preventDefault();
	uploadImages(text.form, e.dataTransfer.files);
});

document.addEventListener("change", function (e) {
	if (!e.target.matches("form.editor [data-image-file]")) return;
	uploadImages(e.target.form, e.target.files);
	e.target.value = "";
});

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
	var code = e.target.closest("form.editor [data-code-block]");
	if (code) codeBlock(editorText(code.form));
	var image = e.target.closest("form.editor [data-image-button]");
	if (image) image.form.querySelector("[data-image-file]").click();
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
	// Not behind an edit link, whose editor has no Link to note (#197).
	if (mod && e.shiftKey && !e.altKey && key === "k" && form.querySelector("[data-note-link]")) {
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
