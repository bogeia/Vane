package taxonomy

import (
	"regexp"
	"slices"
)

// Company title patterns follow AIHOT IDENTITY_LEXICON (see NOTICE).
var companyTitlePatterns = map[string]*regexp.Regexp{
	"openai":       regexp.MustCompile(`(?i:openai|chatgpt|\bgpt-?[o\d]|\bsora\b|\bcodex\b)`),
	"anthropic":    regexp.MustCompile(`(?i:anthropic|\bclaude\b|\b(?:opus|sonnet|haiku)\s*\d+(?:[.\-]\d+)*\b|\bfable\s*\d+(?:[.\-]\d+)*\b|\bmythos\b)`),
	"google":       regexp.MustCompile(`(?i:google|deepmind|\bgemini\b|notebooklm|\bveo\s?\d|\bAlphaFold\b|\bAMIE\b)`),
	"deepseek":     regexp.MustCompile(`(?i:deepseek|深度求索)`),
	"xai":          regexp.MustCompile(`(?i:\bxai\b|\bgrok\b)`),
	"meta":         regexp.MustCompile(`\bMeta\b|(?i:\bmeta\s?ai\b|\bllama\b)`),
	"microsoft":    regexp.MustCompile(`(?i:microsoft|copilot|微软)`),
	"nvidia":       regexp.MustCompile(`(?i:nvidia|英伟达|\bnemotron\b|\bnemo\b|\bblackwell\b|\brubin(?:\s+ultra)?\b|\bcuda\b)`),
	"qwen":         regexp.MustCompile(`(?i:\bqwen|通义|千问)`),
	"hugging-face": regexp.MustCompile(`(?i:hugging\s?face)`),
	"cursor":       regexp.MustCompile(`\bCursor\b`),
	"kimi":         regexp.MustCompile(`(?i:\bkimi\b|月之暗面|\bmoonshot\s?ai\b)`),
	"openrouter":   regexp.MustCompile(`(?i:openrouter)`),
	"minimax":      regexp.MustCompile(`(?i:minimax)`),
	"zhipu":        regexp.MustCompile(`(?i:智谱|\bglm-?[4-9])`),
}

func AssignTopics(title, originalTitle string, subjects, tags []string, category string) []string {
	if len(subjects) == 0 {
		company := ""
		for slug, pattern := range companyTitlePatterns {
			if !pattern.MatchString(title) && !pattern.MatchString(originalTitle) {
				continue
			}
			if company != "" {
				company = ""
				break
			}
			company = slug
		}
		if company != "" {
			subjects = []string{company}
		}
	}
	result := make([]string, 0)
	for _, topic := range Topics {
		if topic.Group == "company" {
			pattern := companyTitlePatterns[topic.Slug]
			if slices.Contains(subjects, topic.Slug) && (len(subjects) == 1 || pattern.MatchString(title) || pattern.MatchString(originalTitle)) {
				result = append(result, topic.Slug)
			}
			continue
		}
		if topic.Tag != "" && slices.Contains(tags, topic.Tag) || topic.Category != "" && category == topic.Category {
			result = append(result, topic.Slug)
		}
	}
	return result
}
