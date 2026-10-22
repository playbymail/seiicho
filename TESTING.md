# Testing environments and accounts

Read this before using or changing the default test accounts, in-memory
databases, or the HTTP account and login helpers. It covers what exists only in
the `development` and `agent` environments. For `go vet` and `go test`, see
[WORKFLOW.md](WORKFLOW.md); for public origins, portals, secure cookies, and
CSRF protection, follow [PROXIES.md](PROXIES.md).

## Environments

Resolve the application environment from `SEIICHO_ENV`, with an explicitly
supplied `--env` flag taking precedence. Everything in this document is
available only in `development` and `agent`; `test`, `production`, and every
other environment must never provide it. Do not enable any of it based on
request headers, hostname, or a query parameter.

## Default accounts

Every new database in `development` or `agent` starts with these accounts:
databases made by `create-database`, and each in-memory database.

| Email | Password | Admin |
| --- | --- | --- |
| `admin@example.com` | `bad-choices-2026` | yes |
| `acct01@example.com` | `worser-choices-2001` | no |
| `acct02@example.com` | `worser-choices-2002` | no |

- These passwords are fixed, public test values, not secrets. They are the only
  credentials allowed in source control; see [AGENTS.md](AGENTS.md). They are
  defined in `internal/devdata`; keep this table in sync with it.
- Never create them in any other environment.
- Never re-seed an existing database. Changed or deleted default accounts stay
  that way; recreate the database to restore them.
- Passwords are still hashed with `bcrypt.MinCost` like any other account.

## In-memory databases

`serve --db :memory:` runs against a fresh, private in-memory database that is
discarded when the server stops. Reject `:memory:` outside `development` and
`agent`. `create-database` and `set-password` always require a database file,
because an in-memory database would vanish when they exit.

## HTTP helpers

### Prefixes

Register the helpers only under the prefix for the resolved environment:

| Environment | Helper prefix |
| --- | --- |
| `development` | `/__dev` |
| `agent` | `/__agent` |
| All others, including `test` and `production` | None |

Do not register `/__dev` in `agent` or `/__agent` in `development`. Unavailable
helper routes must return 404, not redirect to an available prefix.

Both operations below require **POST**. GET, HEAD, and OPTIONS must not create
accounts or sessions. Keep normal CSRF protection enabled; these helpers are not
CSRF bypasses. Agents should use the application's public HTTPS origin (the Amp
portal in an orb), not assume that a user's laptop or loopback URL is reachable.

### Create an account

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
- This helper and the default accounts are the explicit development/agent-only
  exceptions to invite-only account creation. Normal registration must still require a valid invitation.

### Log in

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

When changing the default accounts or in-memory databases, test that they exist
in `development` and `agent` and are refused everywhere else.

When changing the HTTP helpers, cover environment and prefix isolation,
non-POST requests, strict boolean validation, duplicate account creation without
mutation, password verification, authenticated sessions, safe redirects, and
CSRF rejection through HTTP tests. Exercise account creation and login through
the public HTTPS origin in a browser as well. Keep helpers absent in `test`;
HTTP tests can explicitly configure `development` or `agent` when testing them.
