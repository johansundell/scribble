// The open note's Delete (#51). Without JavaScript it opens a page that asks
// first; with it, confirm() asks and the delete is posted straight away.
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
})();
