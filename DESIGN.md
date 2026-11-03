---
name: Vane
description: 灰白、深墨与朱红组织的中文栏目阅读系统
colors:
  bg: "#FAFAF7"
  surface: "#FAFAF7"
  ink: "#232D32"
  ink-2: "#39474C"
  ink-3: "#58676C"
  line: "#DCE1DE"
  line-strong: "#B7C0BB"
  accent: "#9A392B"
  on-accent: "#FFFFFF"
  accent-soft: "#EFE1DC"
  hot: "#9A392B"
  surface-soft: "#F1F2ED"
  dark-bg: "#1A2023"
  dark-surface: "#242C2F"
  dark-ink: "#ECEEE8"
  dark-ink-2: "#C8D1CB"
  dark-ink-3: "#A8B6B0"
  dark-line: "#35413F"
  dark-line-strong: "#556661"
  dark-accent: "#F0A28E"
  dark-on-accent: "#241B18"
  dark-accent-soft: "#35413F"
  dark-hot: "#F0A28E"
  dark-surface-soft: "#252E30"
typography:
  page:
    fontFamily: "\"PingFang SC\", \"Microsoft YaHei\", \"Noto Sans CJK SC\", system-ui, sans-serif"
    fontSize: "29px"
    fontWeight: 650
    lineHeight: 1.4
    letterSpacing: "-.02em"
  title:
    fontFamily: "\"PingFang SC\", \"Microsoft YaHei\", \"Noto Sans CJK SC\", system-ui, sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.55
  panel:
    fontFamily: "\"PingFang SC\", \"Microsoft YaHei\", \"Noto Sans CJK SC\", system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.5
  body:
    fontFamily: "\"PingFang SC\", \"Microsoft YaHei\", \"Noto Sans CJK SC\", system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1.6
  brand:
    fontFamily: "Georgia, \"Songti SC\", \"STSong\", SimSun, serif"
    fontSize: "28px"
    fontWeight: 700
    lineHeight: 1
    letterSpacing: ".3px"
  code:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
    fontSize: "13px"
    lineHeight: 1.8
rounded:
  card: "12px"
  button: "6px"
  filter: "3px"
  small: "4px"
spacing:
  small: "8px"
  medium: "16px"
  section: "24px"
  rail-gap: "clamp(36px, 4.5vw, 64px)"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.on-accent}"
    rounded: "{rounded.button}"
    padding: "8px 18px"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.button}"
    padding: "8px 18px"
  category-filter-selected:
    backgroundColor: "{colors.accent-soft}"
    textColor: "{colors.accent}"
    rounded: "{rounded.filter}"
    padding: "5px 10px"
  reading-item:
    textColor: "{colors.ink}"
    typography: "{typography.title}"
    padding: "19px 0"
  masthead-search:
    textColor: "{colors.ink-3}"
    rounded: "{rounded.button}"
    height: "38px"
    padding: "0 10px"
---

# Design System: Vane

## Overview

**Creative North Star: "栏目式阅读"**

栏目秩序服务中文阅读：灰白阅读地、深墨标题、克制朱红状态与横向版线形成平面层级。标题、正文、元信息各有明确分工，辅助内容不与主内容争夺视觉重量。

使用本机中文无衬线字体呈现内容，衬线字标仅用于 Vane 品牌；原风向标 Logo 原样保留。深浅主题共享语义角色，手机与键盘操作属于系统的一部分。产品事实与能力边界见 [PRODUCT.md](PRODUCT.md)。

**Key Characteristics:**

- 灰白与深墨构成底层，朱红承担交互强调。
- 开放栏目、细分隔与有限正文行长。
- 深浅主题、手机导航与可见焦点保持同一信息层级。

## Colors

### Primary

朱红 `accent` 用于链接、当前导航、选中控件、主要操作与焦点；`on-accent` 用于强调表面文字，`accent-soft` 用于柔和选中底。`hot` 与强调色同值，承担趋势、更新和提醒的上下文语义，不将热度与评分混同。

### Neutral

灰白 `bg` 是阅读地，`surface` 是控件与浮层表面，`surface-soft` 用于空态等弱背景。深墨 `ink` 承载标题与正文，`ink-2` 为摘要和次级正文，`ink-3` 为信源、时间、说明。`line` 是条目分隔，`line-strong` 是结构线与控件边框。更强的栏目版线直接使用 `ink`。

前置令牌的 `dark-` 前缀对应运行时同名变量的深色覆盖；显式深色与跟随系统深色使用相同色表。所有精确色值以前置令牌和 `web/src/styles/base.css` 为准。

**The 语义同位 Rule.** 深浅主题替换同名颜色变量，不另造页面品牌色；第三方 Logo 保留素材本色。

## Typography

**Body Font:** 本机中文优先无衬线栈，见 `typography.body`。
**Display Font:** 内容标题沿用阅读字体；没有另加载展示字体。
**Label/Mono Font:** 标签沿用阅读字体，代码和键盘提示才使用系统等宽栈。

通用页标题使用 `page`，手机缩至 26px；普通资料卡使用 `title`，紧凑行标题为 19px，手机两种资料标题统一为 19px。面板标题使用 `panel`。正文基线使用 `body`，列表摘要行高 1.75；详情、说明文及日报展开正文行高 1.8。辅助文字主要为 12–14px。

品牌字标使用 `brand`，手机为 25px。资料详情标题为 28px/1.5、650，手机为 24px；头条资料为 27px、手机 24px；热点为 21px/1.6，600px 以下为 19px。这些是内容角色差异，不是全局展示字号。日期和分数需要对齐时使用 `tabular-nums`。详情及说明段落限制在 75ch，热点说明可为 65ch。

## Layout

桌面顶部报头最大宽度 1280px、最小高度 84px，以深墨双像素底线建立入口。主内容最大宽度 1190px，外部内边距为 32px 32px 80px。1181px 起，存在辅助栏的页面采用 `minmax(0, 1fr) 250px` 双栏，间距使用 `rail-gap`；辅助栏距顶 28px 吸附，可在视口高度减 56px 的范围滚动。更窄时不渲染辅助栏，也不将其内容移入主列。

960px 及以下切换手机顶部与五项底栏，主区横向留白 18px、顶部留白 24px；底部为导航与安全区预留空间。顶栏高度 64px，底栏避让基准为 68px。精选、热点、主题、日报、更多构成底部入口；收藏与设置由更多进入。详情使用固定底部操作栏并避让回顶按钮。

栏目页眉以横向版线与内容区分。分类在桌面换行、手机为四列网格，七项均可发现。主题网格桌面三列，960px 以下两列，600px 以下单列。更多页桌面两列、手机一列，使用开放分组和横线。520px 下允许带常显视图切换的页眉纵排。

## Elevation & Depth

常规列表、栏目与辅助栏保持平面，不以阴影构造卡片墙。键盘帮助对话框使用浮层阴影与遮罩，跳到正文链接有小阴影；回顶按钮依靠边框与表面色，没有阴影。准确投影参数见 sidecar。

**The 平面栏目 Rule.** 阅读内容优先通过版线、间距和字重分层；浮层投影不扩散到常规列表。

加载进度采用 1.1s 滑动，骨架采用 1.4s 透明度脉冲。没有全站悬浮位移动画。减少动态效果时关闭动画、过渡和平滑滚动，进度改为静态整条。

## Shapes

圆角有明确职责：帮助对话框使用 `card`，主要按钮和搜索使用 `button`，分类使用 `filter`，图片、代码块和部分小标记使用 `small`。常规资料、主题和设置分组保持开放边界；不因代码中名为 card 就套用卡片圆角。回顶按钮和更新圆点使用圆形。胶囊变量仍存在于基础 CSS，但未作为本轮复用形状令牌记录。

## Components

### Buttons

主按钮为朱红底与反色文字，次按钮为表面色、深墨字和结构边框。共享最小高度 42px、字号 14.5px、字重 600；主按钮悬停亮度为 1.08，次按钮悬停边框与文字转强调色。全局可见焦点为强调色 2px 轮廓、偏移 2px。资料工具使用透明图标按钮，以颜色和按下态填充反馈。

### Inputs / Fields

桌面搜索高 38px，以结构边框限定范围，内部透明输入字号 14px；聚焦容器显示 2px 强调轮廓、偏移 3px。手机搜索输入高 42px、字号 16px，使用表面色背景。不是所有输入都套用桌面搜索尺寸。

### Navigation / Chips

桌面导航透明，当前项朱红、650 字重；手机底栏延续同样状态色。分类按钮默认透明、次正文色，选中使用柔和强调底、朱红和 600 字重，通过 `aria-pressed` 表达。视图切换以强调色底线而非填充胶囊表示当前状态。主题页的目录链接为下划线文字入口。

### Cards / Containers

连续资料条目以细底线分隔，默认纵向留白 19px，手机为 18px。标题—摘要—信源时间—阅读动作构成扫读顺序；常规摘要最多两行，展开态和头条可放开。已读仅将标题切换至 `ink-3`。焦点进入条目时底线转强调色。辅助面板无填充背景和卡片外框，以标题、留白与分隔组织。

### Reading Surfaces

资料详情以来源、摘要、正文、讨论及事件分区支持深读，正文入口可折叠；实际可读内容与诚实空态由产品数据决定。日报头条与看点仅为索引，每份资料正文只在一个位置展开；这是日报策略，不要求所有页面都索引化。主题、收藏沿用开放列表。更多、关于、更新日志、Agent 和错误页复用语义字体、版线与按钮，Agent 的代码块保留等宽字体、边框和横向滚动。

## Do's and Don'ts

### Do:

- Do 使用共享语义色，主题切换保持正文、辅助信息和选中状态的层级。
- Do 原样使用现有风向标 Logo，品牌衬线与中文阅读无衬线各司其职。
- Do 保留键盘焦点、手机安全区和减少动态效果支持。
- Do 将列表、详情与日报的不同阅读策略限定在对应页面。

### Don't:

- Don't 为连续资料条目增加常态投影或独立圆角外框。
- Don't 以整体透明度弱化已读内容，或用等宽字体排阅读正文。
- Don't 从草图、旧 DESIGN 或单页特殊值推导全站令牌。


记录依据：`web/src/styles/base.css`、`shell/shell.css`、`shell/Shell.tsx`、`items/{items,feed}.css`、`ui/{page,states,view-switch,home-rail}.css` 及 `pages/{detail,reports,topics,hot,more,agent}.css`。本文件记录完成实现；方向合同保留在 `.impeccable/surfaces/web-src-main-tsx.md`，不以方向草图替代源码值。
