# Seiicho

A strategic, turn-based 4X game played in the browser.

## Running locally

```sh
mkdir -p tmp      # the database directory is never created for you
go run ./cmd/seiichodb create --env development --db tmp/seiicho.db
go run ./cmd/seiichod --env development --db tmp/seiicho.db
```

There are three commands. `seiichodb` owns the database file: it creates it,
applies schema migrations, changes passwords, and activates and deactivates
accounts. `seiichod` is the web server
only; it never creates or migrates a database and refuses to start on one whose
schema is out of date. `seiicho` is the command-line interface to the game
engine: it creates games, generates their clusters of star systems, flags home worlds, seats accounts in them and assigns the seats to home worlds, and lists accounts, games, and seats with their IDs, as tables or JSON; like the server, it only opens an existing, current database. After pulling a schema change, run:

```sh
go run ./cmd/seiichodb migrate --env development --db tmp/seiicho.db
```

`.env.development` sets the backend listener to `127.0.0.1:18443`; Caddy serves
the public HTTPS origin. The tracked environment files do not set the database
path. To drop `--db`, set `SEIICHO_DB` in your untracked
`.env.development.local`.

## Settings

Each setting resolves from an explicit flag, then the environment (including the
files loaded by `internal/dotenv`), then the default.

Long flags take two dashes (`--db`, not `-db`). Every command and subcommand
prints its flags with `-h` or `--help`, for example `seiicho add-seat --help`.

| Flag | Environment | Default | Meaning |
| --- | --- | --- | --- |
| `--env` | `SEIICHO_ENV` | required | `development`, `test`, `production`, or `agent` |
| `--db` | `SEIICHO_DB` | required | Path to the SQLite database file, or `:memory:` (see below) |
| `--addr` | `SEIICHO_ADDR` | `127.0.0.1:18443` | Backend listen address (`host:port`; `seiichod` only) |

`seiichodb create` requires a new file; every other `seiichodb` command,
`seiicho`, and `seiichod` require an existing one. Every command
prints its version with `seiichodb version`, `seiicho version`, or `seiichod
--version`.

## Development conveniences

In the `development` and `agent` environments only, `seiichod --db :memory:` runs
against a throwaway in-memory database, and every new database starts with an
admin account (`admin@example.com` / `bad-choices-2026`) and two player
accounts. See [TESTING.md](TESTING.md) for the full list and the HTTP helpers.

## How-to guides

- [How to assign a seat to a home world](docs/how-to/assign-a-seat.md)
- [How to change an account's password](docs/how-to/change-a-password.md)
- [How to create a game](docs/how-to/create-a-game.md)
- [How to deactivate or reactivate an account](docs/how-to/deactivate-an-account.md)
- [How to find account, game, and seat IDs](docs/how-to/find-ids.md)
- [How to flag a game's home worlds](docs/how-to/flag-home-worlds.md)
- [How to generate a game's cluster](docs/how-to/generate-a-cluster.md)
- [How to seat an account in a game](docs/how-to/seat-an-account.md)

## Reference

- [seiicho JSON output](docs/reference/seiicho-json.md)

## Routes

| Route | Purpose |
| --- | --- |
| `GET /` | Login form, or a welcome page when signed in |
| `POST /login`, `POST /logout` | Start or end a session |
| `GET /api/healthz` | JSON status and version; 503 if the database is unavailable |

In `development` and `agent` only, the testing helpers described in
[TESTING.md](TESTING.md) are served under `/__dev` and `/__agent`.
