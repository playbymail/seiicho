# How to change an account's password

Change an account's password with `seiichodb set-password`. The new password
replaces the old one at once, and the account is signed out everywhere.

The command never takes the password as a flag, which keeps it out of shell
history and process lists. It reads the password from standard input instead.

## Change it at a terminal

```sh
go run ./cmd/seiichodb set-password --env development --db tmp/seiicho.db --email admin@example.com
```

Type the new password at the `New password:` prompt, then again at
`Confirm password:`. Nothing is shown as you type.

```text
updated password for admin@example.com and ended its sessions
```

## Change it from a script

When standard input is not a terminal, the command reads the first line as the
password, without prompting or asking for confirmation. Redirect it from a file
rather than putting the password on the command line:

```sh
go run ./cmd/seiichodb set-password --env development --db tmp/seiicho.db --email admin@example.com < new-password.txt
```

Only the first line is read, and its line ending is dropped.

## After the change

- Every session for the account has ended, so it must sign in again with the
  new password.
- Changing the password of an inactive account does not activate it. To let it
  sign in, see [How to deactivate or reactivate an
  account](deactivate-an-account.md).

## If the command fails

If it fails, the password is unchanged.

### `account not found`

No account has that email. Emails match regardless of case and surrounding
spaces, so check the spelling.

### `passwords do not match`

The two passwords typed at the prompts were different. Run the command again.

### `password must not be empty`

The password was empty, or the first line of the input was blank.

### `database schema is out of date (run seiichodb migrate)`

An older version made the database. Run `seiichodb migrate`, then run the
command again.
