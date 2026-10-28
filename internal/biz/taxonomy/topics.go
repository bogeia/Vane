package taxonomy

import "slices"

type Topic struct {
	Slug          string
	Name          string
	Group         string
	Description   string
	SelectedCount int64
	AllCount      int64
	Tag           string
	Category      string
}

// Topics borrows AIHOT names and descriptions; RAG and search are Vane additions (see NOTICE).
var Topics = []Topic{
	{Slug: "openai", Name: "OpenAI / ChatGPT", Group: "company", Description: "OpenAI 的 GPT 系列模型、ChatGPT 与 Sora 产品更新，以及公司战略与人事变化。"},
	{Slug: "anthropic", Name: "Anthropic / Claude", Group: "company", Description: "Anthropic 的 Claude 系列模型、Claude Code 更新，以及安全研究与公司动向。"},
	{Slug: "google", Name: "Google / Gemini", Group: "company", Description: "Google 与 DeepMind 的 Gemini 模型、Veo 视频生成、前沿研究与各条产品线的 AI 更新。"},
	{Slug: "deepseek", Name: "DeepSeek", Group: "company", Description: "DeepSeek（深度求索）的模型发布、开源权重、技术报告与 API 价格变化。"},
	{Slug: "qwen", Name: "阿里巴巴 / 千问 Qwen", Group: "company", Description: "阿里千问 Qwen 系列的模型发布与开源更新，从旗舰大模型到能装进手机的小模型。"},
	{Slug: "kimi", Name: "月之暗面 / Kimi", Group: "company", Description: "月之暗面的 Kimi 产品更新、K 系列开源模型与长文本技术进展。"},
	{Slug: "minimax", Name: "MiniMax", Group: "company", Description: "MiniMax 的 M 系列大模型、语音与多模态能力，以及开源与商业化进展。"},
	{Slug: "zhipu", Name: "智谱 / GLM", Group: "company", Description: "智谱 GLM 系列模型的发布与开源更新，以及编程、推理能力和商业化进展。"},
	{Slug: "xai", Name: "xAI / Grok", Group: "company", Description: "马斯克旗下 xAI 的 Grok 模型更新、算力集群建设与 X 平台整合。"},
	{Slug: "meta", Name: "Meta / Llama", Group: "company", Description: "Meta 的 Llama 与 Muse 系列模型、超级智能实验室与 Meta AI 产品。"},
	{Slug: "microsoft", Name: "Microsoft / Copilot", Group: "company", Description: "微软的 Copilot 系列产品、Azure AI 云服务，以及与 OpenAI 的合作动向。"},
	{Slug: "nvidia", Name: "NVIDIA 英伟达", Group: "company", Description: "英伟达的 GPU 新品、CUDA 软件生态、机器人平台与 AI 算力供需。"},
	{Slug: "hugging-face", Name: "Hugging Face", Group: "company", Description: "Hugging Face 社区的热门开源模型、数据集、工具与榜单变化。"},
	{Slug: "cursor", Name: "Cursor", Group: "company", Description: "Cursor（Anysphere）的编辑器与编码 Agent 更新、自研模型与定价变化。"},
	{Slug: "openrouter", Name: "OpenRouter", Group: "company", Description: "OpenRouter 平台的新模型上架、价格变化与开发者调用量排行。"},
	{Slug: "agent", Name: "Agent 智能体", Group: "field", Description: "能自己拆解任务、调用工具、一步步把事办完的 AI 智能体：Claude Code、Manus 等应用，各家 Agent 框架与评测。", Tag: "Agent", Category: ""},
	{Slug: "coding", Name: "AI 编码", Group: "field", Description: "用 AI 写代码：编程助手、Vibe Coding、代码模型评测，以及开发方式的变化。", Tag: "编码", Category: ""},
	{Slug: "reasoning", Name: "推理能力", Group: "field", Description: "让大模型先想再答的能力：思维链、推理模型，数学与逻辑评测上的突破和争议。", Tag: "推理", Category: ""},
	{Slug: "multimodal", Name: "多模态", Group: "field", Description: "文字之外的能力：看懂图片、图文混合理解、音视频输入输出的模型与产品进展。", Tag: "多模态", Category: ""},
	{Slug: "voice", Name: "语音与音频", Group: "field", Description: "AI 语音与音频：语音合成、实时语音对话、音乐生成与音频理解的模型与产品。", Tag: "语音", Category: ""},
	{Slug: "video", Name: "AI 视频", Group: "field", Description: "AI 生成和理解视频：文生视频模型、视频编辑工具，以及影视创作的新做法。", Tag: "视频", Category: ""},
	{Slug: "image-gen", Name: "图像生成", Group: "field", Description: "AI 画图与修图：文生图模型更新、图像编辑能力与创作工具。", Tag: "图像生成", Category: ""},
	{Slug: "rag", Name: "RAG 检索增强生成", Group: "field", Description: "用检索到的资料补充模型上下文：知识库、检索流程与生成效果的改进。", Tag: "RAG", Category: ""},
	{Slug: "on-device", Name: "端侧 AI", Group: "field", Description: "直接跑在手机、电脑等设备上的 AI：小模型、在本机运行模型的工具与端侧芯片。", Tag: "端侧", Category: ""},
	{Slug: "data-training", Name: "数据与训练", Group: "field", Description: "大模型是怎么练出来的：数据集构建、合成数据、预训练与后训练方法、算力与训练成本。", Tag: "数据/训练", Category: ""},
	{Slug: "search", Name: "AI 搜索", Group: "field", Description: "用 AI 检索和组织信息：搜索产品、检索能力与信息获取方式的变化。", Tag: "搜索", Category: ""},
	{Slug: "engineering", Name: "部署工程", Group: "field", Description: "怎么把模型跑得更快更省钱：推理加速、显存与成本、服务部署与基础设施选型。", Tag: "部署/工程", Category: ""},
	{Slug: "open-source", Name: "开源生态", Group: "field", Description: "开放权重、能下载下来自己部署的模型，开源框架与 GitHub 上爆火的 AI 项目。", Tag: "开源生态", Category: ""},
	{Slug: "embodied", Name: "具身智能", Group: "field", Description: "装进机器人身体里的 AI：人形机器人、具身基础模型，以及在真实环境里动手干活的能力。", Tag: "具身智能", Category: ""},
	{Slug: "mcp", Name: "MCP 与工具调用", Group: "field", Description: "让 AI 连上外部软件和数据：MCP 协议、function calling 工具调用与各类工具集成。", Tag: "MCP/工具调用", Category: ""},
	{Slug: "model", Name: CategoryModel, Group: "genre", Description: "各家新模型的发布、开源与迭代：旗舰更新、开源权重放出、能力与价格变化。", Tag: "", Category: CategoryModel},
	{Slug: "product", Name: CategoryProduct, Group: "genre", Description: "AI 产品与应用的功能上新、改版与商业化——谁家的产品变强了、变贵了、能用了。", Tag: "", Category: CategoryProduct},
	{Slug: "industry", Name: CategoryIndustry, Group: "genre", Description: "AI 圈的商业大事：融资并购、人事变动、合作与竞争、政策与市场变化。", Tag: "", Category: CategoryIndustry},
	{Slug: "paper", Name: CategoryPaper, Group: "genre", Description: "值得读的 AI 论文与研究成果：架构创新、训练方法、能力测量与理论进展。", Tag: "", Category: CategoryPaper},
	{Slug: "tutorial", Name: CategoryTutorial, Group: "genre", Description: "拿来就能用的实操内容：提示词技巧、工作流搭建、工具用法与踩坑经验。", Tag: "", Category: CategoryTutorial},
	{Slug: "opinion", Name: CategoryOpinion, Group: "genre", Description: "行业关键人物在想什么：创始人访谈、研究者论战与投资人判断。", Tag: "", Category: CategoryOpinion},
}

func ValidTopic(slug string) bool {
	return slices.ContainsFunc(Topics, func(topic Topic) bool { return topic.Slug == slug })
}

const (
	CategoryModel    = "模型"
	CategoryProduct  = "产品"
	CategoryIndustry = "行业"
	CategoryPaper    = "论文"
	CategoryTutorial = "教程"
	CategoryOpinion  = "观点"
	CategoryOther    = "其他"
)
