# seiicho JSON output

With `--json`, the `seiicho` `list` commands print a JSON array of objects, `[]`
when there is nothing to list, and the `show` and `summarize` commands print a
single object. The
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

## Histogram

Printed by `show-histogram`.

| Field | Type | Meaning |
| --- | --- | --- |
| `game_id` | number | Game ID |
| `histogram` | string | `planets-per-star` or `planets-per-star-system` |
| `bins` | array of bins | One bin per number of planets, from the fewest to the most, including bins with a count of 0; `[]` if the game has no cluster |

Each bin has these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `planets` | number | Number of planets |
| `count` | number | Number of stars, or star systems, with that many planets |

## Planet summary

Printed by `summarize-planets`.

| Field | Type | Meaning |
| --- | --- | --- |
| `game_id` | number | Game ID |
| `by` | string | How the planets are grouped: `planet-type` |
| `groups` | array of groups | One group per planet type that has planets, in the order `rocky`, `asteroids`, `giant`; `[]` if the game has no cluster |

Each group has these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `planet_type` | string | `rocky`, `asteroids`, or `giant` |
| `planets` | number | Number of planets of that type |
| `min_habitability` | number | Lowest habitability of those planets |
| `average_habitability` | number | Mean habitability of those planets, unrounded |
| `max_habitability` | number | Highest habitability of those planets |
