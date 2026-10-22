# How to seat an account in a game

Give an account a seat in a game with `seiicho add-seat`, and make an existing
seat a gamemaster seat with `seiicho promote-seat`.

## Before you begin

- The game must exist and be active. `seiicho create-game` prints a new game's
  ID; see [How to create a game](create-a-game.md).
- The account must exist and be active. Both commands take the account's ID,
  not its email. No command lists account IDs yet. In a new `development` or
  `agent` database, the default accounts are `admin@example.com` (1),
  `acct01@example.com` (2), and `acct02@example.com` (3).

If you leave out `--env` or `--db`, the commands use `SEIICHO_ENV` or
`SEIICHO_DB` instead.

## Add a seat

Seat an account as a player:

```sh
go run ./cmd/seiicho add-seat --env development --db tmp/seiicho.db --game 1 --account 2
```

```text
seated account 2 in game 1 as player (seat 2)
```

Add `--gamemaster` to seat it as a gamemaster instead:

```sh
go run ./cmd/seiicho add-seat --env development --db tmp/seiicho.db --game 1 --account 3 --gamemaster
```

```text
seated account 3 in game 1 as gamemaster (seat 3)
```

An account can hold only one seat in a game. `add-seat` never changes an
existing seat; to make a player a gamemaster, promote the seat.

## Promote a seat to gamemaster

```sh
go run ./cmd/seiicho promote-seat --env development --db tmp/seiicho.db --game 1 --account 2
```

```text
promoted account 2 to gamemaster in game 1 (seat 2)
```

Promoting a gamemaster changes nothing and still succeeds:

```text
account 2 is already a gamemaster in game 1 (seat 2)
```

## If the command fails

If either command fails, it changes nothing. Errors name the game and account
you gave, for example `game 1, account 2: account is inactive`.

### `game not found` or `game is inactive`

Check the game ID. No command reactivates a game yet.

### `account not found` or `account is inactive`

Check the account ID. To reactivate an account, see
[How to deactivate or reactivate an account](deactivate-an-account.md).

### `account already has a seat in the game`

From `add-seat`: the account already has a seat in the game, active or not. To
make an active seat a gamemaster seat, use `promote-seat`.

### `account has no active seat in the game`

From `promote-seat`: the account has no seat in the game, or its seat is
inactive. Use `add-seat --gamemaster` to seat an account that has no seat.

### `--game is required` or `--account is required`

Give both `--game` and `--account` as positive IDs.
