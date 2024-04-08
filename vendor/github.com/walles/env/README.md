![CI Status](https://github.com/walles/env/actions/workflows/ci.yml/badge.svg?branch=main)

Functions for parsing environment variable values into typed variables.

# Examples

Note that the resulting values are all typed.

```go
import "github.com/walles/env"

// Enabled will be of type bool
enabled := env.GetOr("ENABLED", strconv.ParseBool, false)

// Duration will be of type time.Duration
duration, err := env.Get("TIMEOUT", time.ParseDuration)

// Username will be of type string. If it's not set in the environment,
// then MustGet will panic.
username := env.MustGet("USERNAME", env.String)

// LuckyNumbers will be of type []int
luckyNumbers, err := env.Get("LUCKY_NUMBERS", env.ListOf(strconv.Atoi, ","))

// FluffyNumber will be a 64 bit precision float
fluffyNumber, err := env.Get("FLOAT", env.WithBitSize(strconv.ParseFloat, 64))

// This will parse both hex and decimal into an uint64
//
// Some allowed number formats: 0xC0de, 1234
number, err := env.Get("HEX", env.WithBaseAndBitSize(strconv.ParseUint, 0, 64))

// Timestamp will be of type time.Time
timestamp, err := env.Get("TIMESTAMP", env.WithTimeSpec(time.Parse, time.RFC3339))

// UsersAndScores will be of type map[string]int.
//
// In this case, "Adam:50,Eva:60" will be parsed into { "Adam":50, "Eva":60 }.
usersAndScores, err := env.Get("USERS_AND_SCORES", Map(env.String, ":", strconv.Atoi, ","))
```

# Installing

To add to your `go.mod` file:

```
go get github.com/walles/env
```

# Alternatives

If you like bindings based APIs better then these ones seem popular:

* <https://github.com/kelseyhightower/envconfig>
* <https://github.com/caarlos0/env>
