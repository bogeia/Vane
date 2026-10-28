import { useState } from 'react'

// 外站图片：只接受 http 和 https，不带 Referer。
// 加载成功才显示；打不开或一直卡住时一直不显示，不留空白也不出现裂图标。
// 不用懒加载：还没显示的图片不会触发懒加载。
export function Picture({ src, className, alt = '' }: { src: string; className?: string; alt?: string }) {
  if (!/^https?:\/\//i.test(src)) return null
  return <LoadedImage key={src} src={src} className={className} alt={alt} />
}

function LoadedImage({ src, className, alt }: { src: string; className?: string; alt: string }) {
  const [state, setState] = useState<'loading' | 'loaded' | 'failed'>('loading')
  if (state === 'failed') return null
  return <img className={className} src={src} alt={alt} referrerPolicy="no-referrer" loading="eager" decoding="async"
    hidden={state !== 'loaded'} onLoad={() => setState('loaded')} onError={() => setState('failed')} />
}
