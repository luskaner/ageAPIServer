# Security policy

## Reporting a vulnerability

Report it privately: use GitHub's **Report a vulnerability** (Security →
Advisories) on this repository, or the contact address published on the
maintainer's profile at <https://github.com/luskaner>. Please do not open a
public issue for an unpatched vulnerability.

## What this project is not

`ageAPIServer` reproduces a client-facing wire protocol. Its credential handling
is a compatibility shim, not authentication:

- **The tickets are not verified.** `Authorization: Steam1.0 <ticket>` is used as
  an opaque user key and `XBL3.0 x=<puid>;<XToken>` as the publisher user id.
  Neither is checked cryptographically, so any well-formed credential creates or
  resolves a user. This is deliberate: the captures of the games carry tickets
  and tokens this server cannot validate.
- **Session cookies are bearer tokens.** `--new-session` mints one for a Steam
  token; whoever holds it is that user until it expires.
- **`X-Forwarded-*` is trusted unconditionally.** It only builds the public URLs
  of the response (`downloadUrl`, `imageThumbnail`, `filePath`); it is not an
  authentication bypass on its own, but a proxy that rewrites those headers can
  point clients at a different host. A trusted-proxy list is future work.
- **The MatchContent rate limit is off by default.** No capture shows a 429, so
  `[MatchContent] RateLimitPerMinute = 0` keeps the observed behaviour; an
  internet-facing deployment should turn it on, or put a real rate limiter in
  front.

Do not expose this server to the internet as the only access control. Put a real
authentication and rate-limiting layer in front of it, or keep it on a trusted
network.
