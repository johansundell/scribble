# 5. Web login with a PIN, plus one shared API token

- **Status:** Accepted (the web login is not built yet)
- **Tickets:** #47

## Context

The JSON API, the CLI and the desktop app use one Bearer token, `AUTH_TOKEN`. A browser needs something a person can type on a phone. johansundell/angel already has a PIN login that works well.

## Decision

- **`WEB_PIN`** (6–12 digits) logs in to the web app. When it is unset, the web app is off and every `/app` path answers 404.
- A right PIN gives a **signed cookie with a fixed expiry** (HMAC-SHA256 under `SESSION_SECRET`, 7 days by default). The expiry is never extended.
- **Per-address limit:** after 5 wrong PINs from one address within 15 minutes, that address gets 429 without the PIN being checked.
- **PIN Alert:** after 20 checked wrong PINs within 24 hours from any addresses, the service logs a warning and every logged-in session sees a banner.
- **Lockout, unlike angel:** at the same 20 wrong PINs, PIN login is turned off for everyone until 24 hours have passed or the service restarts. Existing sessions keep working. In angel a lockout would keep caregivers away from the day's note, so angel only alerts. Here the CLI and the desktop app keep working with `AUTH_TOKEN`, so a locked web login costs little, and it stops guessing from many addresses.
- Gin believes `X-Forwarded-For` only from the proxies in **`TRUSTED_PROXIES`**. By default gin trusts every sender, which would let anyone choose their own address and get past the per-address limit.
- **One shared `AUTH_TOKEN` for every device**, on purpose. There are few devices, and they are tracked by hand. When one is lost, the token is changed on the server and on the other devices.
- The PIN is for the browser only. The API never accepts it.

## Consequences

- A stolen PIN gives full read and write access to every note, as the token does. That is why a 4-digit PIN is not allowed.
- There is no per-device revocation, and the server can't tell devices apart by their token. If that changes (many devices, other people), this decision has to be reopened.
- Sessions are stateless: logging out ends the session on that device only, and changing `SESSION_SECRET` ends all of them.
