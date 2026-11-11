# 14. OAuth2 API authentication with TOTP password grant

- **Status:** Accepted (supersedes static `AUTH_TOKEN` in [ADR 5](0005-web-pin-login-and-shared-api-token.md))
- **Tickets:** #184

## Context

ADR 5 used a single static token, `AUTH_TOKEN`, shared across all API clients (`sn` CLI, desktop GUI, and external scripts). Managing and distributing a static secret manually across machines is inconvenient and creates security risks when rotating or revoking credentials. The web app already authenticates using rotating 6-digit TOTP codes from `WEB_TOTP_SECRET` ([ADR 13](0013-web-totp-authenticator-login.md)).

## Decision

- **OAuth2 Server**: Integrate an OAuth2 authorization server (`github.com/go-oauth2/oauth2`) into `scribble-server` under `/oauth/token`.
- **Resource Owner Password Credentials Grant**:
  - The CLI and desktop GUI authenticate via `grant_type=password`.
  - The `username` is arbitrary in single-user mode.
  - The `password` is a 6-digit TOTP authenticator code, verified against `WEB_TOTP_SECRET`.
  - Rate limiting (5 wrong attempts per 15m) and replay protection apply identically to web login.
- **Token Persistence & Lifetimes**:
  - OAuth2 tokens are persisted in the SQLite database (`oauth_tokens` table) to survive server restarts and deployments.
  - **Access tokens** are short-lived (e.g. 24 hours).
  - **Refresh tokens** are long-lived (e.g. 365 days) and automatically renewed.
- **Client Lifecycles**:
  - **CLI (`sn`)**: Adds `sn login` which prompts for the server URL and authenticator code, acquires OAuth tokens, and writes them to `~/.scribble` (mode `0600`). Expired access tokens are refreshed transparently in the background using the refresh token. `sn logout` removes local tokens.
  - **Desktop GUI (`scribble-gui`)**: The connection dialog prompts for the server URL and authenticator code, acquires tokens, and stores them in the GUI configuration directory.
- **Deprecation of Static `AUTH_TOKEN`**: Static `AUTH_TOKEN` is deprecated and replaced by OAuth2 bearer access tokens.

## Consequences

- API clients authenticate using their phone's authenticator app without needing to view, copy, or configure static server secrets manually.
- Stolen client devices can have their tokens revoked on the server without needing to reconfigure other devices.
- `sn` commands seamlessly maintain an active session via refresh tokens.
