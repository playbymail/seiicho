# Seiicho

## Project

Seiicho is a web application for a strategic, turn-based 4X game: explore,
expand, exploit, and exterminate. Keep the application simple and focused on
playing and managing the game through a browser.

The project is at its initial bootstrap stage. The stack below is the intended
architecture, not a claim that these features are already implemented.

## Required guidance

- Read and follow [WORKFLOW.md](WORKFLOW.md) for development priorities, local
  checks, versioning, and Git permissions. Commit directly to `main`; commits,
  pushes, and GitHub label creation are authorized under that workflow.
- Read and follow [SQLITE3.md](SQLITE3.md) before working on persistence,
  migrations, or database tests.
- Read and follow [PROXIES.md](PROXIES.md) before working on HTTP startup,
  listener configuration, proxy integration, or CSRF protection.
- Do not create GitHub Actions workflows or other GitHub CI actions.

## Application architecture

- Implement the application in Go. The module is `github.com/playbymail/seiicho`;
  use the Go version specified in `go.mod`.
- Provide a simple browser-based UI using server-rendered HTML and HTMX for
  incremental interactions. Prefer ordinary links and forms where sufficient;
  do not introduce a single-page application framework or a separate frontend
  application without a requirement for one.
- Keep game rules and turn processing independent of HTTP handlers and HTML
  rendering. Validate player actions and enforce game rules on the server;
  browser state is not authoritative.
- Store persistent application data in SQLite3 using ZombieZen over modernc,
  with ZombieZen migrations, as specified in `SQLITE3.md`.
- Prefer small, direct implementations over speculative infrastructure. Do not
  invent game mechanics or turn-resolution rules that have not been specified.

## Accounts and registration

- Registration is **invite-only**. Enforce invitation validity on the server
  before creating an account; hiding a public registration link is insufficient.
  Do not introduce an unrestricted registration path.
- Hash passwords with `golang.org/x/crypto/bcrypt`, explicitly passing
  **`bcrypt.MinCost`** to `bcrypt.GenerateFromPassword`. Verify passwords with
  `bcrypt.CompareHashAndPassword`; never store or log plaintext passwords.
- The minimum cost is an intentional project decision: this is a low-risk
  application whose data is not valuable or confidential. Do not silently change
  to `bcrypt.DefaultCost`, a higher cost, or a different hashing scheme.
- Low risk does not remove basic web safeguards: enforce authentication and
  authorization on the server, protect state-changing browser requests against
  CSRF, escape rendered user content, and keep passwords and session credentials
  out of logs and source control.

## Development and verification

Run `.agents/setup` to prepare a fresh orb with Go and module dependencies.
Laptop development uses the user's Caddy configuration; agent orbs use Amp
portals with the `agent` environment, runtime `$PORT`, and `$PUBLIC_URL` as
described in `PROXIES.md`. Do not change Caddy to accommodate an orb or treat
portal checks as verification of Caddy's TLS 1.3 configuration.

Before committing, run:

```sh
go vet ./...
go test ./...
```

For future features, test observable game rules, invitation-gated registration,
password verification, and persistence behavior as applicable. Verify rendered
pages and HTMX interactions in a browser when changing the UI.
