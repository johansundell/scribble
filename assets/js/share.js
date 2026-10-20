// The share pane's Copy link (#83). Without JavaScript the button stays
// hidden and the link is copied from its text field.
(function () {
	"use strict";
	document.querySelectorAll("[data-copy]").forEach(function (button) {
		var field = document.getElementById(button.dataset.copy);
		var status = document.querySelector("[data-copied]");
		if (!field) return;
		button.hidden = false;
		button.addEventListener("click", function () {
			copy(field).then(function () {
				if (status) status.textContent = "Copied";
			}, function () {
				if (status) status.textContent = "Select the link and copy it";
				field.select();
			});
		});
	});

	// copy uses the Clipboard API, which needs HTTPS (or localhost), and
	// falls back to selecting the field and execCommand.
	function copy(field) {
		if (navigator.clipboard && window.isSecureContext) {
			return navigator.clipboard.writeText(field.value);
		}
		field.select();
		return document.execCommand("copy") ? Promise.resolve() : Promise.reject();
	}
})();
