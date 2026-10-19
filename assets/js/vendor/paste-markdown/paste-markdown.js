// @github/paste-markdown v1.5.3, MIT (see LICENSE): dist/bundle.js from the npm
// package, unchanged. https://github.com/github/paste-markdown
var __defProp = Object.defineProperty;
var __name = (target, value) => __defProp(target, "name", { value, configurable: true });

// dist/text.js
function insertText(textarea, text) {
  var _a, _b, _c;
  const before = textarea.value.slice(0, (_a = textarea.selectionStart) !== null && _a !== void 0 ? _a : void 0);
  const after = textarea.value.slice((_b = textarea.selectionEnd) !== null && _b !== void 0 ? _b : void 0);
  let canInsertText = true;
  textarea.contentEditable = "true";
  try {
    canInsertText = document.execCommand("insertText", false, text);
  } catch (error) {
    canInsertText = false;
  }
  textarea.contentEditable = "false";
  if (canInsertText && !textarea.value.slice(0, (_c = textarea.selectionStart) !== null && _c !== void 0 ? _c : void 0).endsWith(text)) {
    canInsertText = false;
  }
  if (!canInsertText) {
    try {
      document.execCommand("ms-beginUndoUnit");
    } catch (e) {
    }
    textarea.value = before + text + after;
    try {
      document.execCommand("ms-endUndoUnit");
    } catch (e) {
    }
    textarea.dispatchEvent(new CustomEvent("change", { bubbles: true, cancelable: true }));
  }
}
__name(insertText, "insertText");

// dist/paste-keyboard-shortcut-helper.js
var skipFormattingMap = /* @__PURE__ */ new WeakMap();
function setSkipFormattingFlag(event) {
  const { currentTarget: el } = event;
  const isSkipFormattingKeys = event.code === "KeyV" && (event.ctrlKey || event.metaKey) && event.shiftKey;
  if (isSkipFormattingKeys || isSkipFormattingKeys && event.altKey) {
    skipFormattingMap.set(el, true);
  }
}
__name(setSkipFormattingFlag, "setSkipFormattingFlag");
function unsetSkipFormattedFlag(event) {
  const { currentTarget: el } = event;
  skipFormattingMap.delete(el);
}
__name(unsetSkipFormattedFlag, "unsetSkipFormattedFlag");
function shouldSkipFormatting(el) {
  var _a;
  const shouldSkipFormattingState = (_a = skipFormattingMap.get(el)) !== null && _a !== void 0 ? _a : false;
  return shouldSkipFormattingState;
}
__name(shouldSkipFormatting, "shouldSkipFormatting");
function installAround(el, installCallbacks, optionConfig) {
  el.addEventListener("keydown", setSkipFormattingFlag);
  for (const installCallback of installCallbacks) {
    installCallback(el, optionConfig);
  }
  el.addEventListener("paste", unsetSkipFormattedFlag);
}
__name(installAround, "installAround");
function uninstall(el) {
  el.removeEventListener("keydown", setSkipFormattingFlag);
  el.removeEventListener("paste", unsetSkipFormattedFlag);
}
__name(uninstall, "uninstall");

// dist/paste-markdown-html.js
function install(el) {
  el.addEventListener("paste", onPaste);
}
__name(install, "install");
function uninstall2(el) {
  el.removeEventListener("paste", onPaste);
}
__name(uninstall2, "uninstall");
function onPaste(event) {
  const transfer = event.clipboardData;
  const { currentTarget: el } = event;
  if (shouldSkipFormatting(el))
    return;
  if (!transfer || !hasHTML(transfer))
    return;
  const field = event.currentTarget;
  if (!(field instanceof HTMLTextAreaElement))
    return;
  if (isWithinUserMention(field)) {
    return;
  }
  let plaintext = transfer.getData("text/plain");
  const textHTML = transfer.getData("text/html");
  const textHTMLClean = textHTML.replace(/\u00A0/g, " ").replace(/\uC2A0/g, " ");
  if (!textHTML)
    return;
  plaintext = plaintext.trim();
  if (!plaintext)
    return;
  const parser = new DOMParser();
  const doc = parser.parseFromString(textHTMLClean, "text/html");
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_ALL, (node) => node.parentNode && isLink(node.parentNode) ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT);
  const markdown = convertToMarkdown(plaintext, walker);
  if (markdown === plaintext)
    return;
  event.stopPropagation();
  event.preventDefault();
  insertText(field, markdown);
}
__name(onPaste, "onPaste");
function convertToMarkdown(plaintext, walker) {
  let currentNode = walker.firstChild();
  let markdown = plaintext;
  let markdownIgnoreBeforeIndex = 0;
  let index = 0;
  const NODE_LIMIT = 1e4;
  while (currentNode && index < NODE_LIMIT) {
    index++;
    const text = isLink(currentNode) ? (currentNode.textContent || "").replace(/[\t\n\r ]+/g, " ") : (currentNode === null || currentNode === void 0 ? void 0 : currentNode.wholeText) || "";
    if (isEmptyString(text)) {
      currentNode = walker.nextNode();
      continue;
    }
    if (!isLink(currentNode)) {
      markdownIgnoreBeforeIndex += text.replace(/[\t\n\r ]+/g, " ").trimStart().length;
      currentNode = walker.nextNode();
      continue;
    }
    const markdownFoundIndex = markdown.indexOf(text, markdownIgnoreBeforeIndex);
    if (markdownFoundIndex >= 0) {
      const markdownLink = linkify(currentNode, text);
      markdown = markdown.slice(0, markdownFoundIndex) + markdownLink + markdown.slice(markdownFoundIndex + text.length);
      markdownIgnoreBeforeIndex = markdownFoundIndex + markdownLink.length;
    }
    currentNode = walker.nextNode();
  }
  return index === NODE_LIMIT ? plaintext : markdown;
}
__name(convertToMarkdown, "convertToMarkdown");
function isWithinUserMention(textarea) {
  const selectionStart = textarea.selectionStart || 0;
  if (selectionStart === 0) {
    return false;
  }
  const previousChar = textarea.value.substring(selectionStart - 1, selectionStart);
  return previousChar === "@";
}
__name(isWithinUserMention, "isWithinUserMention");
function isEmptyString(text) {
  return !text || (text === null || text === void 0 ? void 0 : text.trim().length) === 0;
}
__name(isEmptyString, "isEmptyString");
function isLink(node) {
  var _a;
  return ((_a = node.tagName) === null || _a === void 0 ? void 0 : _a.toLowerCase()) === "a" && node.hasAttribute("href");
}
__name(isLink, "isLink");
function hasHTML(transfer) {
  return transfer.types.includes("text/html");
}
__name(hasHTML, "hasHTML");
function linkify(element, label) {
  const url = element.href || "";
  let markdown = "";
  if (isUserMention(element) || isTeamMention(element)) {
    markdown = label;
  } else if (isSpecialLink(element) || areEqualLinks(url, label)) {
    markdown = url;
  } else {
    markdown = `[${label}](${url})`;
  }
  return markdown;
}
__name(linkify, "linkify");
function isSpecialLink(link) {
  return link.className.indexOf("commit-link") >= 0 || !!link.getAttribute("data-hovercard-type") && link.getAttribute("data-hovercard-type") !== "user";
}
__name(isSpecialLink, "isSpecialLink");
function areEqualLinks(link1, link2) {
  link1 = link1.slice(-1) === "/" ? link1.slice(0, -1) : link1;
  link2 = link2.slice(-1) === "/" ? link2.slice(0, -1) : link2;
  return link1.toLowerCase() === link2.toLowerCase();
}
__name(areEqualLinks, "areEqualLinks");
function isUserMention(link) {
  var _a;
  return ((_a = link.textContent) === null || _a === void 0 ? void 0 : _a.slice(0, 1)) === "@" && link.getAttribute("data-hovercard-type") === "user";
}
__name(isUserMention, "isUserMention");
function isTeamMention(link) {
  var _a;
  return ((_a = link.textContent) === null || _a === void 0 ? void 0 : _a.slice(0, 1)) === "@" && link.getAttribute("data-hovercard-type") === "team";
}
__name(isTeamMention, "isTeamMention");

// dist/paste-markdown-image-link.js
function install2(el) {
  el.addEventListener("dragover", onDragover);
  el.addEventListener("drop", onDrop);
  el.addEventListener("paste", onPaste2);
}
__name(install2, "install");
function uninstall3(el) {
  el.removeEventListener("dragover", onDragover);
  el.removeEventListener("drop", onDrop);
  el.removeEventListener("paste", onPaste2);
}
__name(uninstall3, "uninstall");
function onDrop(event) {
  const transfer = event.dataTransfer;
  if (!transfer)
    return;
  if (hasFile(transfer))
    return;
  if (!hasLink(transfer))
    return;
  const links = extractLinks(transfer);
  if (!links.some(isImageLink))
    return;
  event.stopPropagation();
  event.preventDefault();
  const field = event.currentTarget;
  if (!(field instanceof HTMLTextAreaElement))
    return;
  insertText(field, links.map(linkify2).join(""));
}
__name(onDrop, "onDrop");
function onDragover(event) {
  const transfer = event.dataTransfer;
  if (transfer)
    transfer.dropEffect = "link";
}
__name(onDragover, "onDragover");
function onPaste2(event) {
  const { currentTarget: el } = event;
  if (shouldSkipFormatting(el))
    return;
  const transfer = event.clipboardData;
  if (!transfer || !hasLink(transfer))
    return;
  const links = extractLinks(transfer);
  if (!links.some(isImageLink))
    return;
  event.stopPropagation();
  event.preventDefault();
  const field = event.currentTarget;
  if (!(field instanceof HTMLTextAreaElement))
    return;
  insertText(field, links.map(linkify2).join(""));
}
__name(onPaste2, "onPaste");
function linkify2(link) {
  return isImageLink(link) ? `
![](${link})
` : link;
}
__name(linkify2, "linkify");
function hasFile(transfer) {
  return Array.from(transfer.types).indexOf("Files") >= 0;
}
__name(hasFile, "hasFile");
function hasLink(transfer) {
  return Array.from(transfer.types).indexOf("text/uri-list") >= 0;
}
__name(hasLink, "hasLink");
function extractLinks(transfer) {
  return (transfer.getData("text/uri-list") || "").split("\r\n");
}
__name(extractLinks, "extractLinks");
var IMAGE_RE = /\.(gif|png|jpe?g)$/i;
function isImageLink(url) {
  return IMAGE_RE.test(url);
}
__name(isImageLink, "isImageLink");

// dist/paste-markdown-link.js
var pasteLinkAsPlainTextOverSelectedTextMap = /* @__PURE__ */ new WeakMap();
function install3(el, optionConfig) {
  var _a;
  pasteLinkAsPlainTextOverSelectedTextMap.set(el, ((_a = optionConfig === null || optionConfig === void 0 ? void 0 : optionConfig.defaultPlainTextPaste) === null || _a === void 0 ? void 0 : _a.urlLinks) === true);
  el.addEventListener("paste", onPaste3);
}
__name(install3, "install");
function uninstall4(el) {
  el.removeEventListener("paste", onPaste3);
}
__name(uninstall4, "uninstall");
function onPaste3(event) {
  var _a;
  const { currentTarget: el } = event;
  const element = el;
  const shouldPasteAsPlainText = (_a = pasteLinkAsPlainTextOverSelectedTextMap.get(element)) !== null && _a !== void 0 ? _a : false;
  const shouldSkipDefaultBehavior = shouldSkipFormatting(element);
  if (!shouldPasteAsPlainText && shouldSkipDefaultBehavior || shouldPasteAsPlainText && !shouldSkipDefaultBehavior) {
    return;
  }
  const transfer = event.clipboardData;
  if (!transfer || !hasPlainText(transfer))
    return;
  const field = event.currentTarget;
  if (!(field instanceof HTMLTextAreaElement))
    return;
  const text = transfer.getData("text/plain");
  if (!text)
    return;
  if (!isURL(text))
    return;
  if (isWithinLink(field))
    return;
  const selectedText = field.value.substring(field.selectionStart, field.selectionEnd);
  if (!selectedText.length)
    return;
  if (isURL(selectedText.trim()))
    return;
  event.stopPropagation();
  event.preventDefault();
  insertText(field, linkify3(selectedText, text.trim()));
}
__name(onPaste3, "onPaste");
function hasPlainText(transfer) {
  return Array.from(transfer.types).includes("text/plain");
}
__name(hasPlainText, "hasPlainText");
function isWithinLink(textarea) {
  const selectionStart = textarea.selectionStart || 0;
  if (selectionStart > 1) {
    const previousChars = textarea.value.substring(selectionStart - 2, selectionStart);
    return previousChars === "](";
  } else {
    return false;
  }
}
__name(isWithinLink, "isWithinLink");
function linkify3(selectedText, text) {
  return `[${selectedText}](${text})`;
}
__name(linkify3, "linkify");
function isURL(url) {
  try {
    const parsedURL = new URL(url);
    return removeTrailingSlash(parsedURL.href).trim() === removeTrailingSlash(url).trim();
  } catch (_a) {
    return false;
  }
}
__name(isURL, "isURL");
function removeTrailingSlash(url) {
  return url.endsWith("/") ? url.slice(0, url.length - 1) : url;
}
__name(removeTrailingSlash, "removeTrailingSlash");

// dist/paste-markdown-table.js
function install4(el) {
  el.addEventListener("dragover", onDragover2);
  el.addEventListener("drop", onDrop2);
  el.addEventListener("paste", onPaste4);
}
__name(install4, "install");
function uninstall5(el) {
  el.removeEventListener("dragover", onDragover2);
  el.removeEventListener("drop", onDrop2);
  el.removeEventListener("paste", onPaste4);
}
__name(uninstall5, "uninstall");
function onDrop2(event) {
  const transfer = event.dataTransfer;
  if (!transfer)
    return;
  if (hasFile2(transfer))
    return;
  const textToPaste = generateText(transfer);
  if (!textToPaste)
    return;
  event.stopPropagation();
  event.preventDefault();
  const field = event.currentTarget;
  if (field instanceof HTMLTextAreaElement) {
    insertText(field, textToPaste);
  }
}
__name(onDrop2, "onDrop");
function onDragover2(event) {
  const transfer = event.dataTransfer;
  if (transfer)
    transfer.dropEffect = "copy";
}
__name(onDragover2, "onDragover");
function onPaste4(event) {
  const { currentTarget: el } = event;
  if (shouldSkipFormatting(el))
    return;
  if (!event.clipboardData)
    return;
  const textToPaste = generateText(event.clipboardData);
  if (!textToPaste)
    return;
  event.stopPropagation();
  event.preventDefault();
  const field = event.currentTarget;
  if (field instanceof HTMLTextAreaElement) {
    insertText(field, textToPaste);
  }
}
__name(onPaste4, "onPaste");
function hasFile2(transfer) {
  return Array.from(transfer.types).indexOf("Files") >= 0;
}
__name(hasFile2, "hasFile");
function columnText(column) {
  const noBreakSpace = "\xA0";
  const text = (column.textContent || "").trim().replace(/\|/g, "\\|").replace(/\n/g, " ");
  return text || noBreakSpace;
}
__name(columnText, "columnText");
function tableHeaders(row) {
  return Array.from(row.querySelectorAll("td, th")).map(columnText);
}
__name(tableHeaders, "tableHeaders");
function tableMarkdown(node) {
  const rows = Array.from(node.querySelectorAll("tr"));
  const firstRow = rows.shift();
  if (!firstRow)
    return "";
  const headers = tableHeaders(firstRow);
  const spacers = headers.map(() => "--");
  const header = `${headers.join(" | ")}
${spacers.join(" | ")}
`;
  const body = rows.map((row) => {
    return Array.from(row.querySelectorAll("td")).map(columnText).join(" | ");
  }).join("\n");
  return `
${header}${body}

`;
}
__name(tableMarkdown, "tableMarkdown");
function generateText(transfer) {
  if (Array.from(transfer.types).indexOf("text/html") === -1)
    return;
  const html = transfer.getData("text/html");
  if (!/<table/i.test(html))
    return;
  const start = html.substring(0, html.indexOf("<table"));
  const tableCloseIndex = html.lastIndexOf("</table>");
  if (!start || !tableCloseIndex)
    return;
  const end = html.substring(tableCloseIndex + 8);
  const parser = new DOMParser();
  const parsedDocument = parser.parseFromString(html, "text/html");
  let table = parsedDocument.querySelector("table");
  table = !table || table.closest("[data-paste-markdown-skip]") ? null : table;
  if (!table)
    return;
  const formattedTable = tableMarkdown(table);
  if (!formattedTable)
    return;
  return [start, formattedTable, end].join("").replace(/<meta.*?>/, "");
}
__name(generateText, "generateText");

// dist/paste-markdown-text.js
function install5(el) {
  el.addEventListener("paste", onPaste5);
}
__name(install5, "install");
function uninstall6(el) {
  el.removeEventListener("paste", onPaste5);
}
__name(uninstall6, "uninstall");
function onPaste5(event) {
  const { currentTarget: el } = event;
  if (shouldSkipFormatting(el))
    return;
  const transfer = event.clipboardData;
  if (!transfer || !hasMarkdown(transfer))
    return;
  const field = event.currentTarget;
  if (!(field instanceof HTMLTextAreaElement))
    return;
  const text = transfer.getData("text/x-gfm");
  if (!text)
    return;
  event.stopPropagation();
  event.preventDefault();
  insertText(field, text);
}
__name(onPaste5, "onPaste");
function hasMarkdown(transfer) {
  return Array.from(transfer.types).indexOf("text/x-gfm") >= 0;
}
__name(hasMarkdown, "hasMarkdown");

// dist/index.js
function subscribe(el, optionConfig) {
  installAround(el, [install4, install2, install3, install5, install], optionConfig);
  return {
    unsubscribe: () => {
      uninstall(el);
      uninstall5(el);
      uninstall2(el);
      uninstall3(el);
      uninstall4(el);
      uninstall6(el);
    }
  };
}
__name(subscribe, "subscribe");
export {
  install as installHTML,
  install2 as installImageLink,
  install3 as installLink,
  install4 as installTable,
  install5 as installText,
  subscribe,
  uninstall2 as uninstallHTML,
  uninstall3 as uninstallImageLink,
  uninstall4 as uninstallLink,
  uninstall5 as uninstallTable,
  uninstall6 as uninstallText
};
