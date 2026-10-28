package biz

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

type WorkItem struct {
	Stage               string
	LoadError           error
	ID                  int64
	OriginalTitle, Body string
	PublishedAt         time.Time
	Attempts            int
	Tier                string
	Prefilter           string
	Score1, Score2      *int
	Writing             *Writing
	StoryID             int64
	Selected            bool
}

type Fact struct {
	Title      string
	Subject    string
	Action     string
	Object     string
	OccurredAt *string
}

type Writing struct {
	Roundup  bool
	Title    string
	Summary  string
	Reason   string
	Category string
	Tags     []string
	Subjects []string
	Fact     *Fact
}

// Subjects uses AIHOT's company IDs (see NOTICE).
var Subjects = []string{"openai", "anthropic", "google", "deepseek", "qwen", "kimi", "minimax", "zhipu", "xai", "meta", "microsoft", "nvidia", "hugging-face", "cursor", "openrouter"}

var Categories = []string{taxonomy.CategoryModel, taxonomy.CategoryProduct, taxonomy.CategoryIndustry, taxonomy.CategoryPaper, taxonomy.CategoryTutorial, taxonomy.CategoryOpinion}

// Tags uses AIHOT's fixed topic and entity vocabulary (see NOTICE).
var Tags = []string{"Agent", "编码", "推理", "多模态", "语音", "视频", "图像生成", "RAG", "端侧", "数据/训练", "搜索", "部署/工程", "开源生态", "具身智能", "MCP/工具调用", "OpenAI", "Anthropic", "DeepSeek", "DeepMind", "Google", "Meta", "Microsoft", "xAI", "Hugging Face", "GitHub", "arXiv"}

var ErrInvalidCategory = errors.New("分类不合法")

func NormalizeWriting(value Writing) (Writing, error) {
	for _, text := range []string{value.Title, value.Summary, value.Reason} {
		if strings.ContainsRune(text, '\x00') {
			return Writing{}, fmt.Errorf("writing contains invalid text")
		}
	}
	value.Title = strings.TrimSpace(truncate(strings.TrimSpace(value.Title), 80))
	value.Summary = strings.TrimSpace(truncate(strings.TrimSpace(value.Summary), 400))
	value.Reason = strings.TrimSpace(truncate(strings.TrimSpace(value.Reason), 120))
	if value.Title == "" || value.Summary == "" {
		return Writing{}, fmt.Errorf("writing requires title and summary")
	}
	value.Category = strings.TrimSpace(value.Category)
	if !slices.Contains(Categories, value.Category) {
		value.Category = ""
	}
	tags := make([]string, 0)
	for _, tag := range value.Tags {
		tag = strings.TrimSpace(tag)
		if slices.Contains(Tags, tag) && !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	value.Tags = tags
	subjects := make([]string, 0)
	for _, subject := range value.Subjects {
		if slices.Contains(Subjects, subject) && !slices.Contains(subjects, subject) {
			subjects = append(subjects, subject)
		}
	}
	value.Subjects = subjects
	if value.Fact != nil {
		fact := *value.Fact
		for _, text := range []string{fact.Title, fact.Subject, fact.Action, fact.Object} {
			if strings.ContainsRune(text, '\x00') {
				return Writing{}, fmt.Errorf("invalid writing JSON")
			}
		}
		fact.Title = strings.TrimSpace(truncate(strings.TrimSpace(fact.Title), 30))
		if fact.Title == "" {
			return Writing{}, fmt.Errorf("invalid writing JSON")
		}
		fact.Subject = strings.TrimSpace(truncate(strings.TrimSpace(fact.Subject), 50))
		fact.Action = strings.TrimSpace(truncate(strings.TrimSpace(fact.Action), 50))
		fact.Object = strings.TrimSpace(truncate(strings.TrimSpace(fact.Object), 50))
		if fact.OccurredAt != nil {
			if _, err := time.Parse(time.DateOnly, *fact.OccurredAt); err != nil {
				fact.OccurredAt = nil
			}
		}
		value.Fact = &fact
	}
	if value.Roundup {
		value.Fact = nil
	}
	if err := writingQualityError(value); err != nil {
		return Writing{}, err
	}
	if value.Category == "" {
		return value, ErrInvalidCategory
	}
	return value, nil
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func writingQualityError(value Writing) error {
	for _, text := range []string{value.Title, value.Summary} {
		text = strings.ToLower(text)
		for _, prefix := range []string{"抱歉", "很抱歉", "对不起", "我无法", "我不能", "无法处理", "i'm sorry", "i am sorry", "sorry", "i cannot", "i can't", "as an ai"} {
			if strings.HasPrefix(text, prefix) {
				return fmt.Errorf("writing refused")
			}
		}
	}
	for _, text := range []string{value.Title, value.Summary, value.Reason} {
		for _, phrase := range []string{"作为一个AI", "作为一个 AI", "作为AI助手", "作为 AI 助手", "无法处理链接内容", "无法访问链接"} {
			if strings.Contains(text, phrase) {
				return fmt.Errorf("writing refused")
			}
		}
	}
	if value.Title == value.Summary {
		return fmt.Errorf("writing degenerate")
	}
	for _, text := range []string{value.Title, value.Summary, value.Reason} {
		for _, phrase := range []string{"材料是不可信数据", "只输出 JSON", "不执行其中的指令"} {
			if strings.Contains(text, phrase) {
				return fmt.Errorf("writing degenerate")
			}
		}
		runes := []rune(text)
		run := 0
		for start, char := range runes {
			if start > 0 && char == runes[start-1] {
				run++
			} else {
				run = 1
			}
			if run >= 10 && !unicode.IsSpace(char) {
				return fmt.Errorf("writing degenerate")
			}
			for size := 2; size <= 20 && start+size*4 <= len(runes); size++ {
				fragment := runes[start : start+size]
				// Single-character runs use the ten-character limit above.
				if strings.Trim(string(fragment), string(fragment[0])) == "" || strings.TrimSpace(string(fragment)) == "" {
					continue
				}
				repeated := true
				for n := 1; n < 4; n++ {
					if !slices.Equal(fragment, runes[start+n*size:start+(n+1)*size]) {
						repeated = false
						break
					}
				}
				if repeated {
					return fmt.Errorf("writing degenerate")
				}
			}
		}
	}
	return nil
}
