// Input: 前缀字符串 (如 "app", "compose")
// Output: GenerateAppName 函数 (生成 "{prefix}-{verb}-{adj}-{noun}-{6字符随机}" 格式名称), CleanAppName, AppNameRegex
// Role: 应用名生成器，使用与原 TS 版完全一致的词库和格式，确保命名风格兼容
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
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
