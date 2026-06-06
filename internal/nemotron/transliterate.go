//go:build linux

package nemotron

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// defaultDict maps lowercased Cyrillic transliterations the model tends to
// emit under the ru-RU prompt back to the canonical Latin term a developer
// expects. Whole-word, case-insensitive. Conservative on purpose — only
// unambiguous, single-token terms (a key that is a prefix of a common
// Russian word would mangle that word). Extend via a TSV (see LoadDict).
var defaultDict = map[string]string{
	"кафka":        "Kafka", // (defensive: mixed-script slip)
	"кафка":        "Kafka",
	"прометеус":    "Prometheus",
	"прометей":     "Prometheus",
	"графана":      "Grafana",
	"редис":        "Redis",
	"рейдис":       "Redis",
	"кубернетес":   "Kubernetes",
	"кубернетос":   "Kubernetes",
	"постгрес":     "PostgreSQL",
	"постгрескуль": "PostgreSQL",
	"докер":        "Docker",
	"эластик":      "Elasticsearch",
	"кликхаус":     "ClickHouse",
	"нгинкс":       "Nginx",
	"дженкинс":     "Jenkins",
	"гитлаб":       "GitLab",
	"графкуэль":    "GraphQL",
	"графкуэл":     "GraphQL",
	"монго":        "MongoDB",
	"реббит":       "RabbitMQ",
	"кубер":        "Kubernetes",
	"терраформ":    "Terraform",
	"ансибл":       "Ansible",
	"прометиус":    "Prometheus",
}

var wordRe = regexp.MustCompile(`[\p{L}]+`)

// Transliterate replaces whole-word Cyrillic transliterations with their
// canonical Latin form per dict (case-insensitive). Punctuation, spacing
// and non-matching words are preserved exactly. dict nil → defaultDict.
func Transliterate(text string, dict map[string]string) string {
	if dict == nil {
		dict = defaultDict
	}
	if len(dict) == 0 {
		return text
	}
	return wordRe.ReplaceAllStringFunc(text, func(w string) string {
		if v, ok := dict[strings.ToLower(w)]; ok {
			return v
		}
		return w
	})
}

// LoadDict reads a TSV of "cyrillic<TAB>Canonical" lines (blank lines and
// lines starting with # are skipped) and returns a lowercased-key map. Lets
// the term dictionary be edited without recompiling. Missing file → nil map
// (callers fall back to defaultDict).
func LoadDict(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		if key != "" && val != "" {
			out[key] = val
		}
	}
	return out, sc.Err()
}
