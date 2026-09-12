// The subscription key: where it is read from, and what counts as one.
//
// A missing key is not an error. It means scheduled times only, and the status
// bar says so, which is why nothing here reports one.
package feeds

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// keyNames are the variable names a subscription key has been written under. All
// four are accepted, because a key file written for either implementation of
// this program has to work for the other.
var keyNames = []string{
	"OC_TRANSPO_SUBSCRIPTION_KEY",
	"OCT_SUBSCRIPTION_KEY",
	"OCTRANSPO_SUBSCRIPTION_KEY",
	"SUBSCRIPTION_KEY",
}

// placeholder is what the committed template holds. A person who copied the
// template and never edited it has no key, and saying "scheduled times only" is
// a better answer than asking the endpoint about it.
const placeholder = "your_key_here"

// Key is the subscription key, and empty when there is none.
//
// Three places, in this order: the environment, the working directory's .env,
// then the config directory's. The third is where it lives on a real machine, so
// one key serves both implementations of this program and neither repository
// holds a copy.
//
// A missing key is not an error. It means scheduled times only, and the status
// bar says so.
func Key() string {
	for _, name := range keyNames {
		if key := clean(os.Getenv(name)); key != "" {
			return key
		}
	}

	places := []string{".env"}
	if home, err := os.UserHomeDir(); err == nil {
		places = append(places, filepath.Join(home, ".config", "otransit", ".env"))
	}
	for _, path := range places {
		text, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if key := keyIn(string(text)); key != "" {
			return key
		}
	}
	return ""
}

// keyIn reads a key out of a .env file: one name=value a line, # for a comment,
// and quotes around the value are not part of it.
func keyIn(text string) string {
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if !slices.Contains(keyNames, strings.TrimSpace(name)) {
			continue
		}
		if key := clean(strings.Trim(strings.TrimSpace(value), `"'`)); key != "" {
			return key
		}
	}
	return ""
}

// clean is a key with the spaces off, and empty for the template's stand-in.
func clean(key string) string {
	key = strings.TrimSpace(key)
	if key == placeholder {
		return ""
	}
	return key
}
