# How to deactivate or reactivate an account

Deactivate an account to stop it from signing in, and reactivate it to let it
sign in again. Deactivating does not delete the account or remove it from its
games.

## Deactivate an account

```sh
go run ./cmd/seiichodb deactivate --env development --db tmp/seiicho.db --email acct01@example.com
```

```text
deactivated acct01@example.com and ended its sessions
```

The account is signed out everywhere at once: its sessions end, and signing in
fails with the same "Invalid email or password." message as a wrong password.

Before you deactivate an administrator, check that another administrator is
still active. `seiicho create-game` fails when there is no active
administrator.

## Reactivate an account

```sh
go run ./cmd/seiichodb activate --env development --db tmp/seiicho.db --email acct01@example.com
```

```text
activated acct01@example.com
```

The account can sign in again with its existing password. Sessions that ended
when it was deactivated stay ended, so it must sign in again.

## If the command fails

### `account not found`

No account has that email. Emails match regardless of case and surrounding
spaces, so check the spelling.

### `database schema is out of date (run seiichodb migrate)`

An older version made the database. Run `seiichodb migrate`, then run the
command again.
