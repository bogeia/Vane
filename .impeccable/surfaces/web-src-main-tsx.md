---
version: 1
slug: "web-src-main-tsx"
primary_target: "web/src/main.tsx"
related_targets: ["web/src/shell/Shell.tsx","web/src/items/Feed.tsx","web/src/pages/TopicsPage.tsx","web/src/pages/ItemPage.tsx","web/src/pages/ReportPage.tsx"]
---

# Vane 全站栏目式阅读

Mode: Read。面向广泛 AI 爱好者的桌面与手机阅读站。用户已确认高效扫读、清楚筛选、列表判断与详情深读，选择栏目式阅读。保留现有产品能力、真实内容、原 Logo、键盘与本地阅读状态。范围包括全部阅读与辅助路由，不只首页。

## Direction contract

THESIS: 以紧凑栏目秩序帮助读者判断当天值得读的资料；用标题、事实摘要、信源与时间组织扫描，避免巨幅封面、重复卡片信息和左右竞争的重面板。

OWN-WORLD: 灰白阅读地 #FAFAF7、深墨 #232D32、克制朱红 #9A392B、浅灰结构线 #DCE1DE；原绿色 Logo 保留。中文无衬线正文，栏目标题通过字重与少量横向版线建立身份；不以拟物纸纹、装饰插画或等宽正文代替设计。深色主题保持相同语义层级。

STORY: 读者先识别阅读范围和分类，再扫标题与短摘要，按真实可读状态进入正文或讨论并核对出处。主题在手机可直接进入，日报通过索引一次读完每份资料。评分与热度、资料与事件均保持原有语义。

FIRST VIEWPORT: 桌面以紧凑顶部 Logo、全站导航和搜索开场；列表范围与真实六分类紧随，正文主列与窄辅助列并排，主列占优。资料标题约19–21px，正文约16px，辅助信息至少12px；首屏追求3–4份可判断资料，不压小正文。手机单列、七项分类可发现、底部精选/热点/主题/日报/更多。详情保留栏目页眉、受限正文行长和出处；主题以公司/方向/内容形态直接定位。签名交互是稳定栏目导航与即时可见的筛选范围，运动只服务状态与方向、尊重减少动态效果。

FORM: 用户选择 grounded list #1“科技日报的栏目版”，kind pick；seed:6992c25d3241da23。code-led，批准构图参考 .impeccable/mocks/decision/editorial.html、editorial-topics.html、editorial-detail.html、editorial-mobile.html。手机稿11px辅助文字是已知待修缺陷，正式实现至少12px；图标接口失败时使用已有SourceIcon降级，不能伪造商标。草图中的固定画布与摘录仅用于选择，产品仍完整响应和保留全部内容。

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
