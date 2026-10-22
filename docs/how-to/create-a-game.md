# How to create a game

Create a new game with `seiicho create-game`. The game starts with one seat:
a gamemaster seat held by an administrator.

## Before you begin

- The database must already exist. `seiicho` never creates a database; use
  `seiichodb create` for a new one.
- The database must have at least one active administrator account. A new
  database in the `development` or `agent` environment starts with one,
  `admin@example.com`.

## Create the game

Run `create-game` with the environment and the database file:

```sh
go run ./cmd/seiicho create-game --env development --db tmp/seiicho.db
```

If you leave out `--env` or `--db`, the command uses `SEIICHO_ENV` or
`SEIICHO_DB` instead.

When it succeeds, the command prints the new game's ID, the gamemaster's email,
and the seat ID:

```text
created game 1 with gamemaster admin@example.com (seat 1)
```

## Check who became the gamemaster

You cannot choose the gamemaster. The command seats the active administrator
with the lowest account ID: the oldest active administrator account. It skips
inactive administrators and never seats an account that is not an
administrator. Check the email in the output to confirm the seat went to the
account you expected.

## If the command fails

If it fails, the command creates nothing, so you can fix the problem and run it
again.

### `no active administrator`

The database has no active administrator account. No command can activate an
account or make one an administrator yet. In `development` or `agent`, create a
new database with `seiichodb create`, which starts with the default
administrator, and create the game there.

### `database schema is out of date (run seiichodb migrate)`

An older version made the database. Bring it up to date, then run
`create-game` again:

```sh
go run ./cmd/seiichodb migrate --env development --db tmp/seiicho.db
```

### `database does not belong to this application`

`--db` points to a file that is not a Seiicho database. Check the path.

### `create-game needs a database file`

`seiicho` does not accept `--db :memory:`. Give the path to a database file.
