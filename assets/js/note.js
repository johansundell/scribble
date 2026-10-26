// The open note's Delete (#51) and a history version's Restore (#110).
// Without JavaScript Delete opens a page that asks first, and Restore posts
// straight away; with it, confirm() asks first.
(function () {
	"use strict";
	document.addEventListener("click", function (e) {
		var a = e.target.closest("a[data-confirm]");
		if (!a || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
		e.preventDefault();
		if (!confirm(a.dataset.confirm)) return;
		// The delete page's path is also where the delete is posted.
		var form = document.createElement("form");
		form.method = "post";
		form.action = a.href;
		document.body.appendChild(form);
		form.submit();
	});
	document.addEventListener("submit", function (e) {
		var b = e.submitter;
		if (b && b.dataset.confirm && !confirm(b.dataset.confirm)) e.preventDefault();
	});

	// Times are sent in UTC; show them in the reader's time zone and
	// language, keeping UTC in the tooltip.
	var format = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });
	function localTimes() {
		document.querySelectorAll(".note-view time[datetime]:not([title])").forEach(function (t) {
			var d = new Date(t.getAttribute("datetime"));
			if (isNaN(d)) return;
			t.title = t.textContent;
			t.textContent = format.format(d);
		});
	}
	localTimes();
	// nav.js swaps in other notes without loading this again.
	document.addEventListener("scribble:pane", localTimes);
})();
