# How to find account, game, and seat IDs

`seiicho add-seat` and `seiicho promote-seat` take IDs, not emails. Look them up
with the `list` and `show` commands. All of them only read the database.

Commands named `list` print a table; commands named `show` print one record.
Add `--json` to any of them to print JSON instead, for filtering with `jq`.

If you leave out `--env` or `--db`, the commands use `SEIICHO_ENV` or
`SEIICHO_DB` instead.

## Find an account's ID from its email

```sh
go run ./cmd/seiicho show-account --env development --db tmp/seiicho.db --email acct01@example.com
```

```text
ID      2
EMAIL   acct01@example.com
ADMIN   no
ACTIVE  yes
```

The email match ignores case. If no account has that email, the command fails
with `acct01@example.com: account not found`.

## List every account

```sh
go run ./cmd/seiicho list-accounts --env development --db tmp/seiicho.db
```

```text
ID  EMAIL               ADMIN  ACTIVE
1   admin@example.com   yes    yes
2   acct01@example.com  no     yes
3   acct02@example.com  no     yes
```

Accounts are listed in ID order, active or not. An account with `ACTIVE` `no`
cannot be seated; see
[How to deactivate or reactivate an account](deactivate-an-account.md).

## List every game

```sh
go run ./cmd/seiicho list-games --env development --db tmp/seiicho.db
```

```text
ID  ACTIVE  CREATED              SEATS  GAMEMASTERS
1   yes     2026-10-22 20:30:21  2      admin@example.com
```

Games are listed in ID order, active or not. `CREATED` is in UTC. `SEATS`
counts active seats, and `GAMEMASTERS` lists the emails of the accounts holding
active gamemaster seats, or `-` if there are none.

## List the seats in a game

```sh
go run ./cmd/seiicho list-seats --env development --db tmp/seiicho.db --game 1
```

```text
SEAT  ACCOUNT  EMAIL               ROLE        ACTIVE  CREATED
1     1        admin@example.com   gamemaster  yes     2026-10-22 20:33:56
2     2        acct01@example.com  player      yes     2026-10-22 20:33:56
```

Seats are listed in seat ID order, active or not, so this also shows why
`add-seat` reports that an account already has a seat. The command fails with
`game 1: game not found` if there is no such game.

## Filter the output with jq

Add `--json` and pipe the output to `jq`. Each `list` command prints an array,
empty if there is nothing to list; `show-account` prints an object. The fields
are described in [seiicho JSON output](../reference/seiicho-json.md).

The IDs of the accounts that are not administrators:

```sh
go run ./cmd/seiicho list-accounts --env development --db tmp/seiicho.db --json \
  | jq -r '.[] | select(.is_admin | not) | "\(.id) \(.email)"'
```

```text
2 acct01@example.com
3 acct02@example.com
```

Just the ID of one account, for use in a script:

```sh
go run ./cmd/seiicho show-account --env development --db tmp/seiicho.db --email acct01@example.com --json | jq .id
```

```text
2
```

The emails of a game's active gamemasters:

```sh
go run ./cmd/seiicho list-seats --env development --db tmp/seiicho.db --game 1 --json \
  | jq -r '.[] | select(.is_gm and .is_active) | .email'
```

```text
admin@example.com
```
