// The note list's search and project filter (#49). Without JavaScript the
// form is sent with Enter or Filter and opens the list at /; with it, the
// list is filtered while typing and swapped in, so the open note stays.
(function () {
	"use strict";
	var form = document.querySelector(".list-filter");
	if (!form) return;
	var search = form.querySelector("[name=q]");
	var project = form.querySelector("[name=project]");
	form.querySelector(".filter-submit").hidden = true;

	var timer = 0;
	// seq drops answers that arrive after a newer request was sent.
	var seq = 0;

	// filterURL is this page with the form's filter, in the order the server
	// writes it.
	function filterURL() {
		var v = new URLSearchParams();
		if (project.value) v.set("project", project.value);
		if (search.value.trim()) v.set("q", search.value.trim());
		var qs = v.toString();
		return location.pathname + (qs ? "?" + qs : "");
	}

	function refresh() {
		clearTimeout(timer);
		var url = filterURL();
		var mine = ++seq;
		fetch(url, { credentials: "same-origin" })
			.then(function (res) {
				// Errors and an ended session leave the list as it is; the
				// next page view shows what's wrong.
				if (!res.ok || res.redirected) return;
				return res.text().then(function (html) {
					if (mine !== seq) return;
					var doc = new DOMParser().parseFromString(html, "text/html");
					var items = doc.querySelector(".note-items");
					if (!items) return;
					document.querySelector(".note-items").replaceWith(document.importNode(items, true));
					// The links and forms that carry the filter get the new one.
					doc.querySelectorAll("[data-filtered]").forEach(function (el) {
						var here = document.querySelector('[data-filtered="' + el.dataset.filtered + '"]');
						if (!here) return;
						["href", "action"].forEach(function (attr) {
							if (el.hasAttribute(attr)) here.setAttribute(attr, el.getAttribute(attr));
						});
					});
					history.replaceState(history.state, "", url);
				});
			})
			.catch(function () {});
	}

	search.addEventListener("input", function () {
		clearTimeout(timer);
		timer = setTimeout(refresh, 300);
	});
	project.addEventListener("change", refresh);
	form.addEventListener("submit", function (e) {
		e.preventDefault();
		refresh();
	});
})();
