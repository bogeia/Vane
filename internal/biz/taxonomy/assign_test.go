package taxonomy_test

import (
	"slices"
	"testing"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

func TestAssignTopicsUsesDirectionTagsAndContentForm(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tags     []string
		category string
		want     []string
	}{
		{"multiple directions and form", []string{"Agent", "RAG", "OpenAI", "Agent"}, "论文", []string{"agent", "rag", "paper"}},
		{"category only", nil, "观点", []string{"opinion"}},
		{"unclassified", nil, "", []string{}},
		{"unknown tags and category", []string{"Other"}, "其他", []string{}},
		{"all direction labels", []string{"Agent", "编码", "推理", "多模态", "语音", "视频", "图像生成", "RAG", "端侧", "数据/训练", "搜索", "部署/工程", "开源生态", "具身智能", "MCP/工具调用"}, "模型", []string{"agent", "coding", "reasoning", "multimodal", "voice", "video", "image-gen", "rag", "on-device", "data-training", "search", "engineering", "open-source", "embodied", "mcp", "model"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := taxonomy.AssignTopics("中文标题", "Original", nil, tc.tags, tc.category)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("topics=%v want %v", got, tc.want)
			}
		})
	}
}

func TestAssignTopicsFallsBackToOneCompanyInTitles(t *testing.T) {
	for _, tc := range []struct {
		name, title, originalTitle string
		subjects                   []string
		want                       []string
	}{
		{"title names one company", "OpenAI 发布更新", "", nil, []string{"openai", "agent", "paper"}},
		{"empty subjects", "NVIDIA 发布更新", "", []string{}, []string{"nvidia", "agent", "paper"}},
		{"original title names one company", "新的产品发布", "Microsoft announces Copilot", nil, []string{"microsoft", "agent", "paper"}},
		{"same company in both titles counts once", "OpenAI 发布 ChatGPT 更新", "OpenAI releases Codex", nil, []string{"openai", "agent", "paper"}},
		{"title names multiple companies", "OpenAI 与 Microsoft 合作", "", nil, []string{"agent", "paper"}},
		{"companies differ between titles", "OpenAI 发布更新", "NVIDIA announces update", nil, []string{"agent", "paper"}},
		{"no named company", "新的研究进展", "New research", nil, []string{"agent", "paper"}},
		{"company tag alone is insufficient", "新的研究进展", "", nil, []string{"agent", "paper"}},
		{"nonempty subjects retain authority", "OpenAI 发布更新", "", []string{"anthropic"}, []string{"anthropic", "agent", "paper"}},
		{"unknown subjects disable fallback", "OpenAI 发布更新", "", []string{"unknown"}, []string{"agent", "paper"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := taxonomy.AssignTopics(tc.title, tc.originalTitle, tc.subjects, []string{"Agent", "OpenAI"}, taxonomy.CategoryPaper)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("topics=%v want %v", got, tc.want)
			}
		})
	}
}

func TestAssignTopicsUsesSubjectsAndCompanyTitlePatterns(t *testing.T) {
	for _, tc := range []struct {
		company string
		titles  []string
	}{
		{"openai", []string{"OpenAI", "ChatGPT", "GPT-5", "Sora", "Codex"}},
		{"anthropic", []string{"Anthropic", "CLAUDE", "Opus 4.1", "Sonnet 4", "Haiku 4.5", "Fable 3", "Mythos"}},
		{"google", []string{"Google", "DeepMind", "Gemini", "NotebookLM", "Veo 3", "AlphaFold", "AMIE"}},
		{"deepseek", []string{"DeepSeek", "深度求索"}},
		{"qwen", []string{"Qwen3", "通义", "千问"}},
		{"kimi", []string{"Kimi", "月之暗面", "Moonshot AI"}},
		{"minimax", []string{"MiniMax", "MINIMAX"}},
		{"zhipu", []string{"智谱", "GLM-4", "glm5"}},
		{"xai", []string{"xAI", "Grok"}},
		{"meta", []string{"Meta", "Llama", "META AI"}},
		{"microsoft", []string{"Microsoft", "Copilot", "微软"}},
		{"nvidia", []string{"NVIDIA", "英伟达", "Nemotron", "NeMo", "Blackwell", "Rubin Ultra", "CUDA"}},
		{"hugging-face", []string{"Hugging Face", "HuggingFace"}},
		{"cursor", []string{"Cursor", "使用 Cursor 编码"}},
		{"openrouter", []string{"OpenRouter", "OPENROUTER"}},
	} {
		for _, title := range tc.titles {
			t.Run(tc.company+"/"+title, func(t *testing.T) {
				other := "openai"
				if tc.company == other {
					other = "microsoft"
				}
				got := taxonomy.AssignTopics(title, "", []string{tc.company, other}, nil, "")
				if !slices.Equal(got, []string{tc.company}) {
					t.Fatalf("topics=%v want [%s]", got, tc.company)
				}
				got = taxonomy.AssignTopics("无公司名的中文标题", title, []string{tc.company, other}, nil, "")
				if !slices.Equal(got, []string{tc.company}) {
					t.Fatalf("original title topics=%v want [%s]", got, tc.company)
				}
			})
		}
	}
	for _, tc := range []struct {
		name, title string
		subjects    []string
		want        []string
	}{
		{"multiple subjects only named company", "OpenAI发布更新", []string{"openai", "microsoft"}, []string{"openai"}},
		{"single subject needs no title match", "一次新的产品发布", []string{"anthropic"}, []string{"anthropic"}},
		{"title without subject is insufficient", "OpenAI与Microsoft合作", nil, []string{}},
		{"metadata is not Meta", "metadata pipeline", []string{"meta", "openai"}, []string{}},
		{"lowercase cursor is not Cursor", "move the cursor", []string{"cursor", "openai"}, []string{}},
		{"unversioned opus is not Claude", "opus music", []string{"anthropic", "openai"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := taxonomy.AssignTopics(tc.title, "", tc.subjects, nil, ""); !slices.Equal(got, tc.want) {
				t.Fatalf("topics=%v want %v", got, tc.want)
			}
		})
	}
}

func TestAssignTopicsCoversPublicCompanyCatalog(t *testing.T) {
	var companies []string
	for _, topic := range taxonomy.Topics {
		if topic.Group == "company" {
			companies = append(companies, topic.Slug)
		}
	}
	if len(companies) < 2 {
		t.Fatal("company catalog needs at least two companies for multi-subject coverage")
	}
	for index, company := range companies {
		t.Run(company, func(t *testing.T) {
			if got := taxonomy.AssignTopics("一次新的研究进展", "New research update", []string{company}, nil, ""); !slices.Equal(got, []string{company}) {
				t.Fatalf("single-subject topics=%v want [%s]", got, company)
			}
			other := companies[(index+1)%len(companies)]
			if got := taxonomy.AssignTopics("一次新的研究进展", "New research update", []string{company, other}, nil, ""); len(got) != 0 {
				t.Fatalf("neutral multi-subject topics=%v want none", got)
			}
		})
	}
}
