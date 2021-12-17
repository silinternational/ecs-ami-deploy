package internal

import (
	"encoding/json"
	"fmt"
	"time"
)

// ConvertToOtherType uses json marshal/unmarshal to convert one type to another.
// Output parameter should be a pointer to the receiving struct
func ConvertToOtherType(input, output interface{}) error {
	str, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("failed to convert struct from %T to %T. marshal error: %s", input, output, err.Error())
	}
	if err := json.Unmarshal(str, output); err != nil {
		return fmt.Errorf("failed to convert struct from %T to %T. unmarshal error: %s", input, output, err.Error())
	}

	return nil
}

// IsStringInSlice iterates over a slice of strings, looking for the given
// string. If found, true is returned. Otherwise, false is returned.
func IsStringInSlice(needle string, haystack []string) bool {
	for _, hs := range haystack {
		if needle == hs {
			return true
		}
	}

	return false
}

// CurrentTimestamp returns the current datetime in format YYYYMMDDTHHMMSS
func CurrentTimestamp(layout string) string {
	return time.Now().UTC().Format(layout)
}
