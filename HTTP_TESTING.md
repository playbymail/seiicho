# HTTP testing helpers

Read this before implementing or using HTTP account and login helpers for agent
testing. These are requirements for the future HTTP server, not endpoints that
are already implemented. Follow [PROXIES.md](PROXIES.md) for public origins,
portals, secure cookies, and CSRF protection.

## Environment gating

Resolve the application environment from `SEIICHO_ENV`, with an explicitly
supplied `--env` flag taking precedence. Register helpers only under the prefix
for that resolved environment:

| Environment | Helper prefix |
| --- | --- |
| `development` | `/__dev` |
| `agent` | `/__agent` |
| All others, including `test` and `production` | None |

Do not register `/__dev` in `agent` or `/__agent` in `development`. Unavailable
helper routes must return 404, not redirect to an available prefix. Do not enable
helpers based on request headers, hostname, or a query parameter.

Both operations below require **POST**. GET, HEAD, and OPTIONS must not create
accounts or sessions. Keep normal CSRF protection enabled; these helpers are not
CSRF bypasses. Agents should use the application's public HTTPS origin (the Amp
portal in an orb), not assume that a user's laptop or loopback URL is reachable.

## Create an account

```text
POST /__dev/create-account/<email>?isAdmin=false
POST /__agent/create-account/<email>?isAdmin=false
```

- Encode the email as a URL path segment.
- `isAdmin` defaults to `false` when omitted. When supplied, it must be exactly
  `true` or `false`; reject empty values and other spellings.
- Create the account with the requested administrator status and a randomly
  generated password. Hash it with bcrypt using `bcrypt.MinCost`, as required by
  [AGENTS.md](AGENTS.md). Never log the plaintext password.
- Return an error if the account already exists. Never recreate it, reset its
  password, change its administrator status, or otherwise modify it, including
  when creation requests race.
- This helper is the explicit development/agent-only exception to invite-only
  account creation. Normal registration must still require a valid invitation.

## Log in

```text
POST /__dev/log-me-in/<email>?returnTo=/
POST /__agent/log-me-in/<email>?returnTo=/
```

- Encode the email as a URL path segment and URL-encode the `returnTo` query
  value when necessary.
- Require an existing account; return an error rather than creating one.
- Establish a normal authenticated session for that account, using the same
  secure cookie policy as ordinary login, then redirect to `returnTo`.
- Default `returnTo` to `/` when omitted or empty. Accept only safe relative
  same-origin paths starting with a single `/`. Reject absolute URLs,
  scheme-relative URLs such as `//example.com`, backslashes, control characters,
  and encoded forms that could redirect outside the application. Invalid values
  must not produce an external redirect.

## Verification contract

When implementing these helpers, cover environment and prefix isolation,
non-POST requests, strict boolean validation, duplicate account creation without
mutation, password verification, authenticated sessions, safe redirects, and
CSRF rejection through HTTP tests. Exercise account creation and login through
the public HTTPS origin in a browser as well. Keep helpers absent in `test`;
HTTP tests can explicitly configure `development` or `agent` when testing them.
