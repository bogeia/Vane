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
	{Slug: "openai", Name: "OpenAI / ChatGPT", Group: "company", Description: "OpenAI 的 GPT 模型、ChatGPT 与 Sora 产品更新，以及公司战略和人事变化。"},
	{Slug: "anthropic", Name: "Anthropic / Claude", Group: "company", Description: "Anthropic 的 Claude 模型、Claude Code 更新，以及安全研究和公司动态。"},
	{Slug: "google", Name: "Google / Gemini", Group: "company", Description: "Google 与 DeepMind 的 Gemini、Veo、前沿研究及各产品线的 AI 更新。"},
	{Slug: "deepseek", Name: "DeepSeek", Group: "company", Description: "DeepSeek 的模型发布、开放权重、技术报告与 API 价格变化。"},
	{Slug: "qwen", Name: "阿里巴巴 / 千问 Qwen", Group: "company", Description: "阿里千问 Qwen 系列的模型发布与开源更新，涵盖旗舰模型和端侧小模型。"},
	{Slug: "kimi", Name: "月之暗面 / Kimi", Group: "company", Description: "月之暗面的 Kimi 产品更新、K 系列开源模型与长文本技术进展。"},
	{Slug: "minimax", Name: "MiniMax", Group: "company", Description: "MiniMax 的 M 系列模型、语音与多模态能力，以及开源和商业化进展。"},
	{Slug: "zhipu", Name: "智谱 / GLM", Group: "company", Description: "智谱 GLM 系列的模型发布、开源更新、编程与推理能力及商业化进展。"},
	{Slug: "xai", Name: "xAI / Grok", Group: "company", Description: "xAI 的 Grok 模型更新、算力集群建设与 X 平台整合。"},
	{Slug: "meta", Name: "Meta / Llama", Group: "company", Description: "Meta 的 Llama、Muse 系列模型、超级智能实验室与 Meta AI 产品动态。"},
	{Slug: "microsoft", Name: "Microsoft / Copilot", Group: "company", Description: "微软的 Copilot 产品、Azure AI 云服务，以及与 OpenAI 的合作动态。"},
	{Slug: "nvidia", Name: "NVIDIA 英伟达", Group: "company", Description: "英伟达的 GPU 产品、CUDA 软件生态、机器人平台与 AI 算力供需。"},
	{Slug: "hugging-face", Name: "Hugging Face", Group: "company", Description: "Hugging Face 社区的开源模型、数据集、工具与榜单变化。"},
	{Slug: "cursor", Name: "Cursor", Group: "company", Description: "Cursor 的编辑器、编码智能体、自研模型更新与定价变化。"},
	{Slug: "openrouter", Name: "OpenRouter", Group: "company", Description: "OpenRouter 的模型上架、价格调整与开发者调用量排行。"},
	{Slug: "agent", Name: "Agent 智能体", Group: "field", Description: "AI 智能体的任务规划、工具调用、应用进展，以及开发框架和能力评测。", Tag: "Agent", Category: ""},
	{Slug: "coding", Name: "AI 编码", Group: "field", Description: "AI 编程助手、Vibe Coding、代码模型评测与软件开发方式的变化。", Tag: "编码", Category: ""},
	{Slug: "reasoning", Name: "推理能力", Group: "field", Description: "大模型的思维链与推理能力，以及数学、逻辑评测的进展和争议。", Tag: "推理", Category: ""},
	{Slug: "multimodal", Name: "多模态", Group: "field", Description: "多模态模型与产品的图像理解、图文交互及音视频输入输出能力。", Tag: "多模态", Category: ""},
	{Slug: "voice", Name: "语音与音频", Group: "field", Description: "AI 语音合成、实时语音对话、音乐生成与音频理解的模型和产品进展。", Tag: "语音", Category: ""},
	{Slug: "video", Name: "AI 视频", Group: "field", Description: "AI 视频生成与理解的模型进展、视频编辑工具及影视创作应用。", Tag: "视频", Category: ""},
	{Slug: "image-gen", Name: "图像生成", Group: "field", Description: "AI 图像生成模型、图像编辑能力与相关创作工具的更新。", Tag: "图像生成", Category: ""},
	{Slug: "rag", Name: "RAG 检索增强生成", Group: "field", Description: "检索增强生成中的知识库建设、上下文检索流程与生成效果优化。", Tag: "RAG", Category: ""},
	{Slug: "on-device", Name: "端侧 AI", Group: "field", Description: "手机、电脑等设备上的端侧模型、本地运行工具与 AI 芯片进展。", Tag: "端侧", Category: ""},
	{Slug: "data-training", Name: "数据与训练", Group: "field", Description: "大模型的数据集构建、合成数据、预训练与后训练方法，以及训练算力和成本。", Tag: "数据/训练", Category: ""},
	{Slug: "search", Name: "AI 搜索", Group: "field", Description: "AI 搜索产品、信息检索与组织能力，以及信息获取方式的变化。", Tag: "搜索", Category: ""},
	{Slug: "engineering", Name: "部署工程", Group: "field", Description: "模型推理加速、显存与成本优化、服务部署及基础设施选型。", Tag: "部署/工程", Category: ""},
	{Slug: "open-source", Name: "开源生态", Group: "field", Description: "可下载部署的开放权重模型、开源框架与 GitHub 上的 AI 项目。", Tag: "开源生态", Category: ""},
	{Slug: "embodied", Name: "具身智能", Group: "field", Description: "人形机器人、具身基础模型，以及机器人在真实环境中的感知与行动能力。", Tag: "具身智能", Category: ""},
	{Slug: "mcp", Name: "MCP 与工具调用", Group: "field", Description: "MCP 协议、函数调用及工具集成，涵盖 AI 与外部软件和数据的连接。", Tag: "MCP/工具调用", Category: ""},
	{Slug: "model", Name: CategoryModel, Group: "genre", Description: "AI 模型的发布与迭代、旗舰更新、权重开放，以及能力和价格变化。", Tag: "", Category: CategoryModel},
	{Slug: "product", Name: CategoryProduct, Group: "genre", Description: "AI 产品与应用的功能更新、产品改版、定价调整及商业化进展。", Tag: "", Category: CategoryProduct},
	{Slug: "industry", Name: CategoryIndustry, Group: "genre", Description: "AI 行业的融资并购、人事变动、合作竞争，以及政策和市场变化。", Tag: "", Category: CategoryIndustry},
	{Slug: "paper", Name: CategoryPaper, Group: "genre", Description: "AI 论文与研究成果，涵盖模型架构、训练方法、能力评测和理论进展。", Tag: "", Category: CategoryPaper},
	{Slug: "tutorial", Name: CategoryTutorial, Group: "genre", Description: "AI 提示词技巧、工作流搭建、工具使用方法与实践经验。", Tag: "", Category: CategoryTutorial},
	{Slug: "opinion", Name: CategoryOpinion, Group: "genre", Description: "AI 行业的创始人访谈、研究者讨论与投资人观点。", Tag: "", Category: CategoryOpinion},
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
