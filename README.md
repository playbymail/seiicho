# Seiicho

A strategic, turn-based 4X game played in the browser.

## Running locally

```sh
mkdir -p tmp                                   # the database directory is never created for you
go run ./cmd/seiicho create-database --env development
go run ./cmd/seiicho serve --env development
```

`.env.development` points the database at `tmp/seiicho.db` and the backend
listener at `127.0.0.1:18443`; Caddy serves the public HTTPS origin.

## Settings

Each setting resolves from an explicit flag, then the environment (including the
files loaded by `internal/dotenv`), then the default.

| Flag | Environment | Default | Meaning |
| --- | --- | --- | --- |
| `--env` | `SEIICHO_ENV` | required | `development`, `test`, `production`, or `agent` |
| `--db` | `SEIICHO_DB` | required | Path to the SQLite database file, or `:memory:` (see below) |
| `--addr` | `SEIICHO_ADDR` | `127.0.0.1:18443` | Backend listen address (`host:port`) |

`create-database` requires a new file; `serve` requires an existing one.

## Development conveniences

In the `development` and `agent` environments only, `serve --db :memory:` runs
against a throwaway in-memory database, and every new database starts with an
admin account (`admin@example.com` / `bad-choices-2026`) and two player
accounts. See [TESTING.md](TESTING.md) for the full list and the HTTP helpers.

## Changing a password

```sh
go run ./cmd/seiicho set-password --env development --email admin@example.com
```

The new password is read from standard input: a terminal gets a prompt that
does not echo, and piped input supplies the first line. Changing a password
ends all of that account's sessions.

## Routes

| Route | Purpose |
| --- | --- |
| `GET /` | Login form, or a welcome page when signed in |
| `POST /login`, `POST /logout` | Start or end a session |
| `GET /api/healthz` | JSON status and version; 503 if the database is unavailable |

In `development` and `agent` only, the testing helpers described in
[TESTING.md](TESTING.md) are served under `/__dev` and `/__agent`.
