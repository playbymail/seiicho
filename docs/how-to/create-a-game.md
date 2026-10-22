# How to create a game

Create a new game with `seiicho create-game`. The game starts with one seat:
a gamemaster seat held by an administrator, or by an account you choose.

## Before you begin

- The database must already exist. `seiicho` never creates a database; use
  `seiichodb create` for a new one.
- To let the command pick the gamemaster, the database must have at least one
  active administrator account. A new database in the `development` or
  `agent` environment starts with one, `admin@example.com`.
- To choose the gamemaster yourself, you need the ID of an active account. See
  [How to find account, game, and seat IDs](find-ids.md).
- Choose a seed: any unsigned 64-bit integer, such as `0x0123456789abcdef`.
  Everything random in the game, starting with its cluster of star systems,
  derives from the seed, so the same seed always generates the same game.
  Write the seed down if you may want to reproduce the game.

## Create the game

Run `create-game` with the environment, the database file, and the seed:

```sh
go run ./cmd/seiicho create-game --env development --db tmp/seiicho.db --seed 0x0123456789abcdef
```

The seed may be hexadecimal with a `0x` prefix, or decimal.

If you leave out `--env` or `--db`, the command uses `SEIICHO_ENV` or
`SEIICHO_DB` instead.

When it succeeds, the command prints the new game's ID, the gamemaster's email,
and the seat ID:

```text
created game 1 with gamemaster admin@example.com (seat 1)
```

## Check who became the gamemaster

Without `--with-gamemaster`, the command seats the active administrator with
the lowest account ID: the oldest active administrator account. It skips
inactive administrators and never seats an account that is not an
administrator. Check the email in the output to confirm the seat went to the
account you expected.

## Choose the gamemaster

Give `--with-gamemaster` the account's ID to seat that account as the
gamemaster instead:

```sh
go run ./cmd/seiicho create-game --env development --db tmp/seiicho.db --seed 0xfedcba9876543210 --with-gamemaster 2
```

```text
created game 2 with gamemaster acct01@example.com (seat 2)
```

The account must exist and be active, but it need not be an administrator, and
the database need not have an administrator at all. No administrator is seated.
To add more gamemasters, see [How to seat an account in a game](seat-an-account.md).

The new game has no board yet. To generate one, see
[How to generate a game's cluster](generate-a-cluster.md).

## If the command fails

If it fails, the command creates nothing, so you can fix the problem and run it
again.

### `no active administrator`

You left out `--with-gamemaster`, and the database has no active administrator
account. Either choose the gamemaster with `--with-gamemaster`, or, if an
administrator account exists but is inactive, activate it and run
`create-game` again:

```sh
go run ./cmd/seiichodb activate --env development --db tmp/seiicho.db --email admin@example.com
```

No command can make an account an administrator yet.

### `account 2: account not found` or `account 2: account is inactive`

From `--with-gamemaster`: check the account ID with `seiicho list-accounts`. To
reactivate an account, see
[How to deactivate or reactivate an account](deactivate-an-account.md).

### `--seed is required` or `--seed "…" must be an unsigned 64-bit integer`

Give a seed from 0 to `0xffffffffffffffff`, such as `--seed 0x0123456789abcdef`.

### `--with-gamemaster must be a positive account ID`

Give an account ID, such as `--with-gamemaster 2`, not an email.

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
