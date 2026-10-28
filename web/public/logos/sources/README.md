# 信源发布者 Logo

仅按稳定信源 ID 匹配发布者；没有资源或加载失败时，界面保留信源名的首字头像。不按资料标题、主题归属或名称子串猜测发布者。

## Lobe Icons

来源：[lobehub/lobe-icons](https://github.com/lobehub/lobe-icons)，固定提交 `8b7bbc433d38437fa2b0cca58ae896917162b812`，原始目录 `packages/static-svg/icons/`。以下 SVG 原样保存，遵循上游 [MIT 许可](../companies/LICENSE)。OpenAI、NVIDIA、Hugging Face 复用 `../companies/` 中已保存的对应 SVG，其来源见该目录 README。

| 信源 ID | 本地文件 / 上游同名文件 |
| --- | --- |
| `rss-google-deepmind` | `deepmind-color.svg` |
| `rss-google-research` | `google-color.svg` |
| `rss-google-ai-blog` | `google-color.svg` |
| `rss-microsoft-research` | `microsoft-color.svg` |
| `rss-aws-ml` | `aws-color.svg` |
| `rss-github-ai` | `github.svg` |
| `rss-mistral` | `mistral-color.svg` |

Google 使用 Google / DeepMind 标识，Microsoft Research 使用 Microsoft 标识，不用 Gemini 或 Copilot 产品标识代替发布者。

## 发布者官网资源

以下文件原样下载于 2026-10-27。除特别注明外，资源地址来自对应官网 HTML 的 `rel=icon` 或 `rel=apple-touch-icon`；这些资源**不适用上述 MIT 许可**，标识和版权属于各发布者，仅用于对应信源的辨识，未声明额外授权。

| 信源 ID | 本地文件 | 官网 | 实际资源 URL |
| --- | --- | --- | --- |
| `rss-bair` | `bair.png` | https://bair.berkeley.edu/blog/ | https://bair.berkeley.edu/blog/assets/BAIR_Logo.png |
| `rss-the-verge-ai` | `the-verge.png` | https://www.theverge.com/ | https://www.theverge.com/static-assets/icons/favicon-32x32.png |
| `rss-techcrunch-ai` | `techcrunch.png` | https://techcrunch.com/ | https://techcrunch.com/wp-content/uploads/2015/02/cropped-cropped-favicon-gradient.png?w=32 |
| `rss-ars-technica-ai` | `ars-technica.png` | https://arstechnica.com/ | https://cdn.arstechnica.net/wp-content/uploads/2016/10/cropped-ars-logo-512_480-60x60.png |
| `rss-mit-tech-review-ai` | `mit-tech-review.ico` | https://www.technologyreview.com/ | https://www.technologyreview.com/static/media/favicon.1cfcdb44759a0f93ddf5feb5405dd4cc.ico |
| `rss-the-decoder` | `the-decoder.png` | https://the-decoder.com/ | https://the-decoder.com/resources/favicons/apple-touch-icon-180x180.png?v=3 |
| `rss-simon-willison` | `simon-willison.ico` | https://simonwillison.net/ | https://simonwillison.net/favicon.ico |
| `rss-latent-space` | `latent-space.png` | https://www.latent.space/ | https://latent.space/favicon.png |
| `rss-pytorch` | `pytorch.webp` | https://pytorch.org/ | https://pytorch.org/wp-content/uploads/2024/10/cropped-favicon-32x32.webp |
| `rss-amazon-science` | `amazon-science.png` | https://www.amazon.science/ | https://www.amazon.science/apple-touch-icon.png |
| `rss-mit-news-ml` | `mit-news.ico` | https://news.mit.edu/ | https://news.mit.edu/themes/mit/assets/img/favicon/favicon.ico |
| `rss-qbitai` | `qbitai.ico` | https://www.qbitai.com/ | https://www.qbitai.com/wp-content/themes/liangziwei/imgnew/favicon.ico |
| `rss-import-ai` | `import-ai.png` | https://importai.substack.com/ | https://substackcdn.com/image/fetch/$s_!7eut!,f_auto,q_auto:good,fl_progressive:steep/https%3A%2F%2Fsubstack-post-media.s3.amazonaws.com%2Fpublic%2Fimages%2F031520ec-8765-4d1b-a233-67c6fdb6258a%2Ffavicon-32x32.png |

BAIR 使用官网页头声明的 `img.bair-logo`；Simon Willison 的首页没有显式 icon link，使用官网实际返回的 `/favicon.ico`。PyTorch 官网声明的是 WebP，保留原文件格式。

`rss-geekpark`：官网访问返回 403，隐式 favicon 返回 404，没有保存未经核实的图标，继续显示首字头像。其它自定义或新增 ID 同样回退，不访问外部 favicon 服务。
