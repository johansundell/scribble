// The Copy button on code blocks (#160). Each <pre> in a note gets one at
// its top right, which copies the code and says "✓ Copied" for 2 seconds.
// It runs on load, after nav.js swaps in a note (scribble:pane) and after
// app.js fills the Preview tab (scribble:preview). Without JavaScript the
// code is selected by hand.
(function () {
	"use strict";
	var LABEL = "Copy";
	var DONE = "✓ Copied";
	var FAILED = "Copy failed";

	// write puts text on the clipboard. navigator.clipboard needs a secure
	// context (HTTPS or localhost), so on a plain-HTTP LAN address it falls
	// back to execCommand.
	function write(text) {
		if (navigator.clipboard && window.isSecureContext) {
			return navigator.clipboard.writeText(text);
		}
		return new Promise(function (resolve, reject) {
			var area = document.createElement("textarea");
			area.value = text;
			area.setAttribute("readonly", "");
			area.style.position = "fixed";
			area.style.opacity = "0";
			document.body.appendChild(area);
			area.select();
			var ok = false;
			try {
				ok = document.execCommand("copy");
			} catch (e) {}
			area.remove();
			if (ok) {
				resolve();
			} else {
				reject(new Error("copy failed"));
			}
		});
	}

	function add(pre) {
		if (pre.parentElement.classList.contains("code-block")) return;
		var wrap = document.createElement("div");
		wrap.className = "code-block";
		pre.replaceWith(wrap);
		wrap.appendChild(pre);
		var button = document.createElement("button");
		button.type = "button";
		button.className = "code-copy";
		button.textContent = LABEL;
		button.setAttribute("aria-label", "Copy code");
		var timer;
		button.addEventListener("click", function () {
			var code = pre.querySelector("code") || pre;
			var done = function (label) {
				button.textContent = label;
				clearTimeout(timer);
				timer = setTimeout(function () {
					button.textContent = LABEL;
				}, 2000);
			};
			write(code.textContent).then(
				function () {
					done(DONE);
				},
				function () {
					done(FAILED);
				},
			);
		});
		wrap.appendChild(button);
	}

	function enhance() {
		document.querySelectorAll(".note-body pre").forEach(add);
	}

	enhance();
	document.addEventListener("scribble:pane", enhance);
	document.addEventListener("scribble:preview", enhance);
})();
