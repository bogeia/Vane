package watchwords

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ValidateWords reports only rule names; configured expressions may contain secrets.
func ValidateWords(groups [][]string) error {
	for i, group := range groups {
		ordinary := false
		for _, word := range group {
			term := word
			if strings.HasPrefix(term, "+") || strings.HasPrefix(term, "!") {
				term = term[1:]
			} else {
				ordinary = true
			}
			if strings.TrimSpace(term) == "" {
				return fmt.Errorf("words[%d] failed nonempty_word", i)
			}
			if strings.HasPrefix(term, "/") && strings.HasSuffix(term, "/") && len(term) >= 2 {
				if _, err := regexp.Compile("(?i:" + term[1:len(term)-1] + ")"); err != nil {
					return fmt.Errorf("words[%d] failed valid_regexp", i)
				}
			}
		}
		if !ordinary {
			return fmt.Errorf("words[%d] failed ordinary_word", i)
		}
	}
	return nil
}

// MatchWords matches each title independently and returns positive terms from matching groups.
func MatchWords(groups [][]string, title, originalTitle string) (bool, []string) {
	var hits []string
	matched := false
	for _, group := range groups {
		ordinary, required, excluded := false, true, false
		var groupHits []string
		for _, word := range group {
			term := word
			kind := byte(0)
			if strings.HasPrefix(term, "+") || strings.HasPrefix(term, "!") {
				kind = term[0]
				term = term[1:]
			}
			hit := false
			if len(term) >= 2 && strings.HasPrefix(term, "/") && strings.HasSuffix(term, "/") {
				pattern, err := regexp.Compile("(?i:" + term[1:len(term)-1] + ")")
				hit = err == nil && (pattern.MatchString(title) || pattern.MatchString(originalTitle))
			} else if term != "" {
				hit = strings.Contains(strings.ToLower(title), strings.ToLower(term)) || strings.Contains(strings.ToLower(originalTitle), strings.ToLower(term))
			}
			switch kind {
			case '+':
				required = required && hit
			case '!':
				excluded = excluded || hit
			default:
				ordinary = ordinary || hit
			}
			if hit && kind != '!' && !slices.Contains(groupHits, term) {
				groupHits = append(groupHits, term)
			}
		}
		if ordinary && required && !excluded {
			matched = true
			for _, term := range groupHits {
				if !slices.Contains(hits, term) {
					hits = append(hits, term)
				}
			}
		}
	}
	return matched, hits
}
