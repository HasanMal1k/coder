package assistant

import (
	"regexp"
	"strconv"
	"strings"
)

// handles are the names agents are given, in the order they are handed out:
// short, easy to type after an @, and unlike each other. The first twenty
// are also in the migration that gave agents from before their names.
var handles = []string{
	"tom", "ava", "max", "zoe", "sam", "mia", "leo", "ivy", "ben", "eva",
	"jay", "kai", "noa", "rex", "uma", "ted", "liv", "finn", "nia", "ray",
	"ida", "hal", "ada", "gus", "isla", "otto", "rosa", "abe", "cora", "dan",
	"ella", "fred", "gwen", "hugo", "iris", "jack", "kate", "luke", "maya", "ned",
	"olga", "pete", "ruby", "seth", "tara", "vic", "wade", "yara", "zack", "amos",
	"bea", "cal", "dora", "eli", "faye", "gil", "hana", "ian", "june", "kit",
	"lena", "milo", "nora", "omar", "pia", "quin", "rhea", "saul", "tess", "vera",
}

// assistantHandle is how the owner addresses the assistant itself. No agent
// is given it.
const assistantHandle = "assistant"

// nextHandle returns the first name not among taken: from the list, then the
// list again with a number after each name.
func nextHandle(taken []string) string {
	used := make(map[string]bool, len(taken))
	for _, h := range taken {
		used[h] = true
	}

	for round := 1; ; round++ {
		for _, name := range handles {
			candidate := name
			if round > 1 {
				candidate += strconv.Itoa(round)
			}
			if !used[candidate] {
				return candidate
			}
		}
	}
}

// mentionPattern finds "@name" in what the owner wrote. A mention starts the
// text or follows something that isn't part of a word or an address, so
// "me@tom.com" is not one.
var mentionPattern = regexp.MustCompile(`(?:^|[^\w@.])@([a-z][a-z0-9-]*)`)

// mentions returns the names mentioned in text, each once, in the order they
// first appear, lowercased.
func mentions(text string) []string {
	var found []string
	seen := make(map[string]bool)

	for _, m := range mentionPattern.FindAllStringSubmatch(strings.ToLower(text), -1) {
		name := strings.TrimRight(m[1], "-")
		if !seen[name] {
			seen[name] = true
			found = append(found, name)
		}
	}

	return found
}
