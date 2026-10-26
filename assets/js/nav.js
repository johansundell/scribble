// Opening a note (#107). Without JavaScript a note's title, backlink or
// [[link]] loads the whole page; with it, only the note pane is swapped in,
// so the page doesn't go blank and the list keeps its scroll position. Pages
// with a form in the note pane (the editor, Rename, Share, Delete) still load
// in full, so editor.js never holds a form that is gone.
(function () {
	"use strict";
	// A note's page, and the open-by-title page that sends on to it.
	var NOTE = /^\/note\/\d+$/;
	var TITLE = /^\/title\//;
	// The note pane's pages with a form, which load in full.
	var FORM = "form, .note-form";

	// seq drops answers that arrive after a newer request was sent.
	var seq = 0;
	// shownPath is the path of the page on show.
	var shownPath = location.pathname;
	// loaded holds the scripts already on the page; a note pane brings its
	// own (note.js), which only run once.
	var loaded = {};
	document.querySelectorAll("script[src]").forEach(function (s) {
		loaded[s.getAttribute("src")] = true;
	});

	// swappable reports whether the note pane shows a note or nothing yet.
	function swappable() {
		var view = document.querySelector(".note-view");
		return !!view && !view.querySelector(FORM);
	}

	// noteURL is where a goes, if it is a link to a note, or null.
	function noteURL(a) {
		if (!a.closest(".note-items, .backlinks, .note-body") || a.target || a.hasAttribute("download") || a.classList.contains("missing")) {
			return null;
		}
		var u = new URL(a.href);
		if (u.origin !== location.origin || !(NOTE.test(u.pathname) || TITLE.test(u.pathname))) return null;
		return u;
	}

	// highlight marks the list's title for path, if it lists it.
	function highlight(path) {
		document.querySelectorAll(".note-items a").forEach(function (a) {
			if (new URL(a.href).pathname === path) {
				a.setAttribute("aria-current", "page");
			} else {
				a.removeAttribute("aria-current");
			}
		});
	}

	// open swaps in the note at url, or the list's empty pane for Back to
	// the home page. push adds it to the history; a failure loads the page
	// instead.
	function open(url, push) {
		var mine = ++seq;
		if (NOTE.test(url.pathname)) highlight(url.pathname);
		fetch(url.href, { credentials: "same-origin" })
			.then(function (res) {
				// A [[link]] is sent on to its note; anything else, such as an
				// ended session sent to the login, loads in full.
				var to = new URL(res.url);
				if (!res.ok || !(NOTE.test(to.pathname) || (!push && to.pathname === "/"))) throw new Error("not a note");
				return res.text().then(function (html) {
					if (mine !== seq) return;
					var doc = new DOMParser().parseFromString(html, "text/html");
					if (!swap(doc)) throw new Error("not a note");
					if (push && to.href !== location.href) history.pushState(null, "", to.href);
					shownPath = to.pathname;
					highlight(to.pathname);
				});
			})
			.catch(function () {
				if (mine !== seq) return;
				if (push) {
					location.assign(url.href);
				} else {
					location.reload();
				}
			});
	}

	// swap puts doc's note pane in place of this one, and reports whether
	// doc had one to show.
	function swap(doc) {
		var view = doc.querySelector(".note-view");
		var main = doc.querySelector(".app-main");
		if (!view || !main || view.querySelector(FORM)) return false;
		var scripts = [];
		view.querySelectorAll("script[src]").forEach(function (s) {
			scripts.push(s.getAttribute("src"));
			s.remove();
		});
		document.querySelector(".note-view").replaceWith(document.importNode(view, true));
		// Phones show the note pane only with has-note.
		document.querySelector(".app-main").className = main.className;
		document.title = doc.title;
		// On phones the page scrolls, not the note pane.
		window.scrollTo(0, 0);
		// For the scripts already loaded, such as note.js.
		document.dispatchEvent(new Event("scribble:pane"));
		scripts.forEach(function (src) {
			if (loaded[src]) return;
			loaded[src] = true;
			var s = document.createElement("script");
			s.src = src;
			document.body.appendChild(s);
		});
		return true;
	}

	// On window, so it runs after editor.js has had its say on the document.
	window.addEventListener("click", function (e) {
		if (e.defaultPrevented || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
		var a = e.target.closest && e.target.closest("a[href]");
		if (!a || !swappable()) return;
		var url = noteURL(a);
		if (!url) return;
		e.preventDefault();
		open(url, true);
	});

	// Back and Forward between the notes swapped in here, and the home page.
	// A step that only changes the #fragment, or the list's filter, is left
	// alone.
	window.addEventListener("popstate", function () {
		if (location.pathname === shownPath) return;
		shownPath = location.pathname;
		if ((NOTE.test(location.pathname) || location.pathname === "/") && swappable()) {
			open(new URL(location.href), false);
		} else {
			location.reload();
		}
	});
})();
