# 13. Web login with TOTP authenticator codes

- **Status:** Accepted (supersedes web PIN login in [ADR 5](0005-web-pin-login-and-shared-api-token.md))
- **Tickets:** #172

## Context

ADR 5 chose a static PIN (`WEB_PIN`) for web login. A static PIN is convenient on a phone, but vulnerable to shoulder surfing and brute-force replay across time windows. Google Authenticator and compatible TOTP apps (RFC 6238) offer rotating 6-digit codes that change every 30 seconds, providing significantly stronger security while remaining quick and convenient to type into a browser.

## Decision

- **`WEB_TOTP_SECRET`** (a base32-encoded secret) replaces `WEB_PIN` as the web application authenticator. When `WEB_TOTP_SECRET` is unset, web login is disabled and web application routes are turned off.
- **RFC 6238 Standard**: Uses standard TOTP (HMAC-SHA1, 6 digits, 30-second time steps).
- **Setup & Device Enrollment**:
  - The setup page at `/totp-setup` displays the QR code and the plaintext base32 secret with a copy button.
  - To prevent unauthorized access to the secret, viewing `/totp-setup` requires entering the server's `AUTH_TOKEN`.
  - The QR code encodes standard URI parameters: `otpauth://totp/Scribble:<host>?secret=<SECRET>&issuer=Scribble`.
- **Clock Drift & Replay Protection**:
  - Validates codes within $\pm 1$ time step (a 90-second acceptable window) to tolerate minor device clock drift.
  - An accepted code cannot be reused within the same time step (anti-replay).
- **Rate Limiting & Lockout**:
  - Retains the existing rate-limiting thresholds from ADR 5:
    - **Per-address limit**: 5 wrong codes from one address within 15 minutes returns 429.
    - **Global Lockout**: 20 wrong codes across all addresses within 24 hours locks out web login for everyone until 24 hours pass or the service restarts.
    - **Login Alert**: A warning banner on active sessions and a service log alert after 20 failed attempts.
- **Public demo** (#175): `PUBLIC_DEMO=true` shows the QR code and the secret on `/login` itself, without `AUTH_TOKEN`, and turns off the global lockout and the Login Alert, so one visitor can't lock everyone out of a demo whose secret is public anyway. The per-address limit stays. It is off by default and is never for a server with private notes.
- **Session Cookies**:
  - Successfully entering a valid TOTP code sets the signed, fixed-expiry session cookie (HMAC-SHA256 under `SESSION_SECRET`, 7-day lifetime), identical to ADR 5.

## Consequences

- Web login requires an authenticator app (Google Authenticator, Aegis, 1Password, etc.).
- `WEB_PIN` is removed/deprecated; administrators must configure `WEB_TOTP_SECRET` to enable web access.
- Setup is straightforward: admins can scan the QR code via `/totp-setup` using their server's `AUTH_TOKEN`.
- The JSON API and clients (`sn`, desktop GUI) continue using `AUTH_TOKEN` independently.
