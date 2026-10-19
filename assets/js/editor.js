// The note editor (#50). Without JavaScript the form posts on its own; with
// it, leaving with unsaved changes asks first, and the buttons save with
// fetch so a failed request never loses the text.
(function () {
	"use strict";
	var UNREACHABLE = "The server can't be reached. Your text is still here; try again.";
	var LEAVE = "Leave without saving? Your changes will be lost.";

	var form, text, initial;
	// leaving is set once a save went through or the user agreed to leave,
	// so the browser doesn't ask again.
	var leaving = false;

	function dirty() {
		return !!form && !leaving && (form.dataset.dirty === "true" || text.value !== initial);
	}

	function init() {
		form = document.querySelector("form.editor");
		if (!form) return;
		text = form.querySelector("textarea[name=body]");
		initial = text.value;
		leaving = false;
		// The cursor goes at the end of the first line: the title.
		var end = text.value.indexOf("\n");
		if (end < 0) end = text.value.length;
		text.focus();
		text.setSelectionRange(end, end);
		form.addEventListener("submit", submit);
		// Rename is only offered with nothing unsaved: save first.
		var rename = form.querySelector("[data-rename]");
		if (rename) {
			text.addEventListener("input", function () {
				rename.hidden = dirty();
			});
		}
		var keep = form.querySelector("[data-keep-editing]");
		if (keep) {
			keep.addEventListener("click", function (e) {
				e.preventDefault();
				form.querySelector(".conflict").hidden = true;
				form.querySelector(".editor-error").hidden = true;
				text.focus();
			});
		}
	}

	function show(selector, msg) {
		var el = form.querySelector(selector);
		if (msg !== undefined) el.textContent = msg;
		el.hidden = false;
	}

	function setBusy(busy) {
		form.querySelectorAll("button").forEach(function (b) {
			b.disabled = busy;
		});
	}

	// replace swaps in the editor the server answered with (a refused save,
	// or Reload), keeping this page and its URL.
	function replace(html) {
		var doc = new DOMParser().parseFromString(html, "text/html");
		var view = doc.querySelector(".note-view");
		if (!view || !view.querySelector("form.editor")) return false;
		// This script is already running; a second copy would ask twice.
		view.querySelectorAll("script").forEach(function (s) {
			s.remove();
		});
		document.querySelector(".note-view").replaceWith(document.importNode(view, true));
		document.title = doc.title;
		init();
		return true;
	}

	function submit(e) {
		e.preventDefault();
		var data = new FormData(form);
		if (e.submitter && e.submitter.name) data.append(e.submitter.name, e.submitter.value);
		form.querySelector(".editor-error").hidden = true;
		setBusy(true);
		// Not form.action: the buttons are named "action", and that would
		// return them instead.
		fetch(form.getAttribute("action"), { method: "POST", body: new URLSearchParams(data), credentials: "same-origin" })
			.then(function (res) {
				if (res.redirected) {
					// A post from an ended session is sent to the login.
					if (new URL(res.url).pathname === "/app/login") {
						show(".session-ended");
						return;
					}
					leaving = true;
					location.assign(res.url);
					return;
				}
				return res.text().then(function (html) {
					if (replace(html)) return;
					show(".editor-error", res.status >= 502 && res.status <= 504
						? UNREACHABLE
						: "Saving failed (" + res.status + "). Your text is still here; try again.");
				});
			}, function () {
				show(".editor-error", UNREACHABLE);
			})
			.then(function () {
				if (!leaving) setBusy(false);
			});
	}

	window.addEventListener("beforeunload", function (e) {
		if (dirty()) {
			e.preventDefault();
			e.returnValue = "";
		}
	});

	// In-app links (the list, Cancel, Edit) ask too. Links that open a new
	// tab, or stay on this page, don't leave it.
	document.addEventListener("click", function (e) {
		var a = e.target.closest("a[href]");
		if (!a || a.target === "_blank" || a.getAttribute("href").charAt(0) === "#") return;
		if (e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
		if (!dirty()) return;
		if (confirm(LEAVE)) {
			leaving = true;
		} else {
			e.preventDefault();
		}
	});

	init();
})();
