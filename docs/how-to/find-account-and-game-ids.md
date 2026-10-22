# How to find account and game IDs

`seiicho add-seat` and `seiicho promote-seat` take IDs, not emails. Look the
IDs up with `seiicho list-accounts` and `seiicho list-games`. Both only read the
database.

If you leave out `--env` or `--db`, the commands use `SEIICHO_ENV` or
`SEIICHO_DB` instead.

## Find an account's ID from its email

```sh
go run ./cmd/seiicho list-accounts --env development --db tmp/seiicho.db --email acct01@example.com
```

```text
ID  EMAIL               ADMIN  ACTIVE
2   acct01@example.com  no     yes
```

The email match ignores case. If no account has that email, the command fails
with `acct01@example.com: account not found`.

## List every account

Leave out `--email` to list all accounts, active or not, in ID order:

```sh
go run ./cmd/seiicho list-accounts --env development --db tmp/seiicho.db
```

```text
ID  EMAIL               ADMIN  ACTIVE
1   admin@example.com   yes    yes
2   acct01@example.com  no     yes
3   acct02@example.com  no     yes
```

An account with `ACTIVE` `no` cannot be seated; see
[How to deactivate or reactivate an account](deactivate-an-account.md).

## List every game

```sh
go run ./cmd/seiicho list-games --env development --db tmp/seiicho.db
```

```text
ID  ACTIVE  CREATED              SEATS  GAMEMASTERS
1   yes     2026-10-22 20:30:21  2      admin@example.com,acct02@example.com
```

Games are listed in ID order, active or not. `CREATED` is in UTC. `SEATS`
counts active seats, and `GAMEMASTERS` lists the emails of the accounts holding
active gamemaster seats, or `-` if there are none.
