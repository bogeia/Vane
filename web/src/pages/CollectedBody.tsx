export function CollectedBody({ body, openFromLink = false }: { body: string; openFromLink?: boolean }) {
  const paragraphs = body.replace(/\r\n?/g, '\n').split(/\n(?:[^\S\n]*\n)+/).filter(paragraph => paragraph.trim())
  return <details id="body" className="detail-block detail-body" open={openFromLink || undefined}>
    <summary>采集正文</summary>
    <p className="detail-body-note">采集内容可能不完整，请以原文为准。</p>
    {paragraphs.length
      ? paragraphs.map((paragraph, index) => <p className="detail-body-text" key={index}>{paragraph}</p>)
      : <p className="detail-note">暂无可展示的采集正文，可通过“阅读原文”继续阅读。</p>}
  </details>
}
