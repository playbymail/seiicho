# seiicho JSON output

With `--json`, the `seiicho` `list` commands print a JSON array of objects, `[]`
when there is nothing to list, and `show` commands print a single object. The
output is indented and ends with a newline. Times are RFC 3339 strings in UTC,
such as `"2026-10-22T15:04:05Z"`. Password hashes are never included.

## Account

Printed by `list-accounts` and `show-account`.

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | number | Account ID |
| `email` | string | Email address, as stored |
| `is_admin` | boolean | Whether the account is an administrator |
| `is_active` | boolean | Whether the account may sign in and be seated |

## Game

Printed by `list-games`.

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | number | Game ID |
| `is_active` | boolean | Whether the game is active |
| `created_at` | string | When the game was created |
| `seats` | number | Number of active seats |
| `gamemasters` | array of strings | Emails of the accounts holding active gamemaster seats, in seat ID order; `[]` if none |

## Seat

Printed by `list-seats`, which includes inactive seats.

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | number | Seat ID |
| `game_id` | number | Game ID |
| `account_id` | number | ID of the account holding the seat |
| `email` | string | Email of the account holding the seat |
| `is_gm` | boolean | Whether it is a gamemaster seat; otherwise a player seat |
| `is_active` | boolean | Whether the seat is active |
| `created_at` | string | When the seat was created |
