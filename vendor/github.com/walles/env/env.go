package env

import (
	"fmt"
	"os"
	"strings"
)

type parseError struct {
	message string
}

func (pe parseError) Error() string {
	return pe.message
}

// Get a value from environment and run it through the parse function. Return
// the result if there was one.
//
// If the parsing fails or if the variable was not set an error will be
// returned.
//
// Parse errors vs not-set errors can be told apart using env.IsParseError().
//
// # Example Usage
//
//	port, err := env.Get("PORT", strconv.Atoi)
func Get[V any](environmentVariableName string, parse func(string) (V, error)) (V, error) {
	rawValue, found := os.LookupEnv(environmentVariableName)
	if !found {
		var noResult V
		return noResult, fmt.Errorf("Environment variable not set: %s", environmentVariableName)
	}

	parsedValue, err := parse(rawValue)
	if err != nil {
		var noResult V
		return noResult, parseError{
			message: fmt.Sprintf("Parsing %s value: %v", environmentVariableName, err),
		}
	}

	return parsedValue, nil
}

// Returns true if this was a parse error returned by env.Get(). False usually
// means an env.Get() error is because the variable was not set.
//
// False will also be returned if the error was nil (not an error), or if it's
// not from env.Get().
func IsParseError(err error) bool {
	_, isParseError := err.(parseError)
	return isParseError
}

// Get a value from environment and run it through the parse function. Return
// the result if there was one.
//
// If the parsing fails or if the variable was not set then the fallback value
// will be returned.
//
// # Example Usage
//
//	port := env.GetOr("PORT", strconv.Atoi, 8080)
func GetOr[V any](environmentVariableName string, parse func(string) (V, error), fallback V) V {
	rawValue, found := os.LookupEnv(environmentVariableName)
	if !found {
		return fallback
	}

	parsedValue, err := parse(rawValue)
	if err != nil {
		return fallback
	}

	return parsedValue
}

// Get a value from environment and run it through the parse function. Return
// the result if there was one.
//
// If the parsing fails or if the variable was not set then this function will
// panic.
//
// # Example Usage
//
//	port := env.MustGet("PORT", strconv.Atoi)
func MustGet[V any](environmentVariableName string, parse func(string) (V, error)) V {
	parsedValue, err := Get(environmentVariableName, parse)
	if err != nil {
		panic(err)
	}

	return parsedValue
}

// Helper function for reading lists from environment variables.
//
// # Example Usage
//
//	numbers, err := env.Get("NUMBERS", env.ListOf(strconv.Atoi, ","))
func ListOf[V any](parse func(string) (V, error), separator string) func(string) ([]V, error) {
	return func(stringWithSeparators string) ([]V, error) {
		separatedString := strings.Split(stringWithSeparators, separator)

		var result []V
		for index, part := range separatedString {
			parsedValue, err := parse(part)
			if err != nil {
				return nil, fmt.Errorf("Element %d: %w", index+1, err)
			}

			result = append(result, parsedValue)
		}

		return result, nil
	}
}

// Helper function for reading maps from environment variables.
//
// # Example Usage
//
// This can be used to parse a string of the form "a:5,b:9,c:42":
//
//	mapping, err := env.Get("MAPPING", env.Map(env.String, ":", strconv.Atoi, ","))
func Map[K comparable, V any](
	parseKey func(string) (K, error),
	keyValueSeparator string,
	parseValue func(string) (V, error),
	entriesSeparator string,
) func(string) (map[K]V, error) {
	return func(stringWithMap string) (map[K]V, error) {
		entries := strings.Split(stringWithMap, entriesSeparator)

		result := make(map[K]V)
		var empty map[K]V
		for index, entry := range entries {
			rawKeyAndValue := strings.Split(entry, keyValueSeparator)
			if len(rawKeyAndValue) != 2 {
				return empty, fmt.Errorf(`Element %d doesn't have exactly one separator ("%s"): %s`,
					index+1,
					keyValueSeparator,
					entry,
				)
			}

			rawKey := rawKeyAndValue[0]
			rawValue := rawKeyAndValue[1]

			parsedKey, err := parseKey(rawKey)
			if err != nil {
				return empty, fmt.Errorf("Key %d: %w", index+1, err)
			}

			parsedValue, err := parseValue(rawValue)
			if err != nil {
				return empty, fmt.Errorf("Value %d: %w", index+1, err)
			}

			result[parsedKey] = parsedValue
		}

		return result, nil
	}
}

// Helper function for parsing floats and similar from environment variables.
//
// # Example Usage
//
//	number, err := env.Get("FLOAT", env.WithBitSize(strconv.ParseFloat, 64))
func WithBitSize[V any](parse func(string, int) (V, error), bitSize int) func(string) (V, error) {
	return func(raw string) (V, error) {
		return parse(raw, bitSize)
	}
}

// Helper function for parsing ints of different bases from environment
// variables.
//
// Pro tip: Passing base 0 with [strconv.ParseInt] and [strconv.ParseUint]
// will make them try to figure out the base by themselves.
//
// # Example Usage
//
//	number, err := env.Get("HEX", env.WithBaseAndBitSize(strconv.ParseUint, 0, 64))
//
// [strconv.ParseInt]: https://pkg.go.dev/strconv#ParseInt
// [strconv.ParseUint]: https://pkg.go.dev/strconv#ParseUint
func WithBaseAndBitSize[V any](parse func(string, int, int) (V, error), base, bitSize int) func(string) (V, error) {
	return func(raw string) (V, error) {
		return parse(raw, base, bitSize)
	}
}

// Helper function for parsing timestamps using [time.Parse] from environment
// variables.
//
// # Example Usage
//
//	timestamp, err := Get("TEST", WithTimeSpec(time.Parse, time.RFC3339))
//
// [time.Parse]: https://pkg.go.dev/time#Parse
func WithTimeSpec[V any](parse func(string, string) (V, error), layout string) func(string) (V, error) {
	return func(raw string) (V, error) {
		return parse(layout, raw)
	}
}

// Helper function for reading strings from the environment.
//
// # Example Usage
//
//	userName, err := env.Get("USERNAME", env.String)
func String(input string) (string, error) {
	return input, nil
}
