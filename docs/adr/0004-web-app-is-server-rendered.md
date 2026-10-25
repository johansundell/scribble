# 4. The web app is server-rendered

- **Status:** Accepted (not built yet)
- **Tickets:** #46, #47, #92, #107
- **Update (#92):** the web app moved from `/app` to the root, `/`. Its note pages are at `/note/<id>`, apart from the JSON API's `/notes/<id>`; the health check stays at `/healthz`.
- **Update (#107):** opening a note from the list, a backlink or a [[link]] swaps in only the note pane (with `pushState`), so the page doesn't go blank. Every page is still rendered by the server, and anything else loads in full.

## Context

The web app at `/app` should work like the desktop app in a browser, on phones too. One person maintains it, next to a Go server, a Go CLI and a Go desktop app.

## Decision

- Pages are **Go templates** served under `/app`, embedded in the binary like the existing `tmpl/` templates. The web handlers call the store directly.
- A little **plain JavaScript** is used only where it is needed: live search, the unsaved-changes check, saving without losing the text, asking before a delete, keyboard shortcuts, the editor toolbar and the theme switcher (an inline script in `<head>` sets the saved theme before the page paints). There is no build step, no JavaScript framework and no SPA.
- Markdown is rendered **in Go** with goldmark, with raw HTML escaped, so a note can't put scripts into the page.
- No external fonts or scripts. Copied third-party code (GitHub's MIT-licensed `markdown-toolbar-element` and `paste-markdown`) lives in the repo.

## Consequences

- The web app is part of the Go codebase and its tests, and ships in the same binary.
- The PIN screen works without JavaScript.
- Rich client features, like an offline mode, would be hard to add later. That is accepted ([ADR 6](0006-web-app-is-online-only.md)).
