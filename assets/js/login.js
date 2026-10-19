// The on-screen keypad on the PIN screen. Without JavaScript it stays hidden
// and the field works on its own.
(function () {
	var field = document.getElementById("pin");
	var keypad = document.querySelector(".keypad");
	if (!field || !keypad) return;
	keypad.hidden = false;
	keypad.addEventListener("click", function (e) {
		var key = e.target.closest("button");
		if (!key) return;
		if (key.dataset.digit !== undefined) {
			if (field.value.length < field.maxLength) field.value += key.dataset.digit;
		} else if (key.dataset.key === "back") {
			field.value = field.value.slice(0, -1);
		} else if (key.dataset.key === "clear") {
			field.value = "";
		}
	});
})();
