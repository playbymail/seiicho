# seiicho JSON output

With `--json`, the `seiicho` `list` commands print a JSON array of objects, `[]`
when there is nothing to list, and the `show`, `summarize`,
`flag-home-world`, and `assign-seat` commands print a single object. The
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
| `empire_id` | number or null | Faction ID of the seat's empire; `null` if it has none |
| `empire_name` | string or null | Name of the seat's empire; `null` if it has none |
| `home_planet_id` | number or null | Planet ID of the empire's home world; `null` if the seat has no empire |

## Home world

Printed by `flag-home-world`.

| Field | Type | Meaning |
| --- | --- | --- |
| `game_id` | number | Game ID |
| `planet_id` | number | ID of the planet flagged as a home world |
| `x` | number | x coordinate of the planet's star system |
| `y` | number | y coordinate of the planet's star system |
| `z` | number | z coordinate of the planet's star system |
| `star` | string | Letter of the planet's star within its system, from `A` |
| `orbit` | number | The planet's orbit, from 1 |

## Histogram

Printed by `show-histogram`.

| Field | Type | Meaning |
| --- | --- | --- |
| `game_id` | number | Game ID |
| `histogram` | string | `planets-per-star`, `planets-per-star-system`, `deposit-quantity`, `deposit-yield`, or `deposits-per-planet` |
| `bins` | array of bins | The bins, described below, including bins with a count of 0; `[]` if the game has no cluster, or for a deposit histogram if its cluster has no deposits |

For `planets-per-star` and `planets-per-star-system`, there is one bin per
number of planets, from the fewest to the most. Each bin has these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `planets` | number | Number of planets |
| `count` | number | Number of stars, or star systems, with that many planets |

For `deposit-quantity`, the bins are on a logarithmic scale. Each runs from 1,
2, or 5 times a power of 10 to just below the next such bound, except that the
lowest, for 100,000, starts at the smallest possible deposit, 99,999. The bins
run from the one holding the smallest deposit to the one holding the largest.
Each bin has these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `min_quantity` | number | Smallest quantity in the bin |
| `max_quantity` | number | Largest quantity in the bin |
| `count` | number | Number of deposits with a quantity in the bin |

For `deposit-yield`, there is one bin per yield from 1 to 25, or to the
highest yield if a home world's is higher. Each bin has these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `yield_pct` | number | Yield, as a percentage |
| `count` | number | Number of deposits with that yield |

For `deposits-per-planet`, there is one bin per number of deposits from 1 to
45. Each bin has these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `deposits` | number | Number of deposits |
| `count` | number | Number of planets with that many deposits |

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

## Deposit summary

Printed by `summarize-deposits`.

| Field | Type | Meaning |
| --- | --- | --- |
| `game_id` | number | Game ID |
| `by` | string | How the deposits are grouped: `resource`, `planet-type`, or `planets-per-star` |
| `groups` | array of groups | The groups, described below; `[]` if the game has no cluster or its cluster has no deposits |

Quantities and wealth are numbers of units. A planet's wealth is the sum of
its deposits' quantities, and a star's wealth is the sum of its planets'.
Shares are fractions from 0 to 1. Averages are unrounded.

With `by` set to `resource`, there is one group per resource that has
deposits, in the order `gold`, `fuel`, `mets`, `nmts`. Each group has these
fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `resource` | string | `gold`, `fuel`, `mets`, or `nmts` |
| `quantity` | number | Total quantity of the resource |
| `share` | number | The resource's share of the total quantity of all the game's deposits |
| `deposits` | number | Number of deposits of the resource |
| `min_per_star` | number | Lowest quantity of the resource on one star; stars without it count as 0 |
| `average_per_star` | number | Mean quantity of the resource per star |
| `max_per_star` | number | Highest quantity of the resource on one star |

With `by` set to `planet-type`, there is one group per planet type whose
planets have deposits, in the order `rocky`, `asteroids`, `giant`. Each group
has these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `planet_type` | string | `rocky`, `asteroids`, or `giant` |
| `planets` | number | Number of planets of that type |
| `min_deposits` | number | Fewest deposits on one of those planets |
| `average_deposits` | number | Mean number of deposits per planet |
| `max_deposits` | number | Most deposits on one of those planets |
| `min_wealth` | number | Lowest wealth of one of those planets |
| `average_wealth` | number | Mean wealth per planet |
| `max_wealth` | number | Highest wealth of one of those planets |
| `shares` | object | Each resource's share of the type's wealth, with the fields `gold`, `fuel`, `mets`, and `nmts` |

With `by` set to `planets-per-star`, there is one group for each number of
planets that a star with deposits has, from the fewest to the most. Each group
has these fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `planets` | number | Number of planets |
| `stars` | number | Number of stars with that many planets |
| `average_per_star` | number | Mean wealth of those stars |
| `average_per_planet` | number | Mean wealth of their planets |

## Empire

Printed by `assign-seat`.

| Field | Type | Meaning |
| --- | --- | --- |
| `game_id` | number | Game ID |
| `seat_id` | number | ID of the seat assigned |
| `faction_id` | number | Faction ID of the new empire |
| `name` | string | Name of the empire |
| `race_id` | number | ID of the race of the empire's home world |
| `planet_id` | number | Planet ID of the empire's home world |
| `x` | number | x coordinate of the home world's star system |
| `y` | number | y coordinate of the home world's star system |
| `z` | number | z coordinate of the home world's star system |
| `star` | string | Letter of the home world's star within its system, from `A` |
| `orbit` | number | The home world's orbit, from 1 |
