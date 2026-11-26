# 16. Images live in the database behind public hash URLs

- **Status:** Accepted
- **Tickets:** #201

## Context

Notes are plain Markdown ([ADR 1](0001-notes-are-plain-markdown.md)), and Markdown can show images, but until now a note could only show one hosted somewhere else. People want to drop a screenshot or a photo into a note from the web app, including behind an edit link ([ADR 15](0015-share-links-can-grant-edit.md)), and see it in the note, its share link and its history.

An image needs somewhere to live, a URL a note can hold, and a rule for who may fetch it. The web app's pages need a session, the share pages a token in the path; neither fits an `<img>` that should show on every page the note appears on.

## Decision

- **Storage:** images live in the SQLite database, in an `images` table (`id` = the SHA-256 of the bytes as hex, `mime`, `data`, `created_at`), added by a forward-only migration. Backups ([#62](../../README.md#backups)) pick it up with no changes. The same bytes are stored once. Nothing deletes an image, not even deleting every note that shows it.
- **Reference:** a note holds `![<file name, or "image">](/images/<sha256>.<ext>)`. The type, and so the extension, comes from the file's bytes, never its name. The renderer shows images from `http(s)` and from `/images/<sha256>.<ext>`, and turns any other source into its text.
- **Serving:** `GET /images/<sha256>.<ext>` needs no login: a 256-bit hash can't be guessed, so knowing the URL is the key, as a share token is. It is sent with `Cache-Control: public, max-age=31536000, immutable` (the bytes behind a hash never change), `X-Content-Type-Options: nosniff`, `X-Robots-Tag: noindex` and `Content-Security-Policy: sandbox`, so a browser never runs one as a page. A name whose extension isn't the stored type answers 404. The route exists only while the web app is on, like `/s/`.
- **Upload:** `POST /app/images` with the web app's session (and its same-origin check), and `POST /e/<token>/images` with an edit link's token as the key, like the link's save (no same-origin check). Revoking the link stops further uploads; what was uploaded stays. Only PNG, JPEG, GIF and WebP, up to 10 MB; SVG, which can carry script, is refused with everything else. A JPEG's APP1 segments (EXIF and XMP, which can hold GPS position) are removed before it is hashed, so the hash is of what is stored and stripping again changes nothing.
- **Editor:** an Image button, dropping a file on the text and pasting one all share one upload, which holds the place with `![Uploading <name>…]()` until it is replaced by the reference, or removed, with an error, if the upload fails.
- **Export and import** ([#117](../../README.md#export-and-import)) carry the images as `images/<sha256>.<ext>`. Import checks each as an upload is checked, and that its name is the one its bytes give it. The archive limit goes from 64 MB to 512 MB.
- **Unchanged:** the FileMaker replica, the desktop app, `sn` and offline copies keep the Markdown and show the reference as text.
- There is no token-authenticated upload in the API yet; `POST /images` can come later.

## Consequences

- Anyone with an image's URL can open it, logged in or not. The URL is as private as the notes and pages it appears in.
- Revoking a share or edit link doesn't hide the images its holder has seen: they keep working for whoever has their URLs.
- Images are never deleted, so the database only grows. Removing one means editing the database by hand.
- Images grow the database, its backups and its exports. A 10 MB cap per image keeps any one of them in check.
- FileMaker container fields are a possible later step, if the replica should show images too.
