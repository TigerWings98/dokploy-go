package schema

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

// AppNameRegex validates app names: letters, numbers, dots, underscores, hyphens only.
var AppNameRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

const AppNameMessage = "App name can only contain letters, numbers, dots, underscores and hyphens"

var (
	verbs = []string{
		"back-up", "bypass", "hack", "override", "compress", "copy",
		"navigate", "index", "connect", "generate", "quantify",
		"calculate", "synthesize", "transmit", "program", "parse",
		"reboot", "input", "read",
	}
	adjectives = []string{
		"auxiliary", "primary", "back-end", "digital", "open-source",
		"virtual", "cross-platform", "redundant", "online", "haptic",
		"multi-byte", "bluetooth", "wireless", "solid-state", "neural",
		"optical", "mobile", "1080p",
	}
	nouns = []string{
		"driver", "protocol", "bandwidth", "panel", "microchip",
		"program", "port", "card", "array", "interface",
		"system", "sensor", "firewall", "hard-drive", "pixel",
		"alarm", "feed", "monitor", "application", "transmitter",
		"bus", "circuit", "capacitor", "matrix",
	}
	customAlphabet = "abcdefghijklmnopqrstuvwxyz123456789"
)

// GenerateAppName generates a unique app name with the given prefix.
func GenerateAppName(prefix string) string {
	verb := verbs[rand.Intn(len(verbs))]
	adj := adjectives[rand.Intn(len(adjectives))]
	noun := nouns[rand.Intn(len(nouns))]
	suffix := randomString(6, customAlphabet)
	return fmt.Sprintf("%s-%s-%s-%s-%s", prefix, verb, adj, noun, suffix)
}

// CleanAppName normalizes an app name.
func CleanAppName(appName string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(appName, " ", "-")))
}

func randomString(length int, alphabet string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}
