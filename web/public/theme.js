// 在第一次绘制前设好外观，选了深色的读者不会先看到浅色。
// CSP 不允许内联脚本，所以单独放成同源文件，在 <head> 里同步加载。
// 取值和改写 theme-color 的规则与 src/shell/theme.ts 保持一致。
(function () {
  var theme = 'system'
  try {
    var saved = localStorage.getItem('vane-theme')
    if (saved === 'light' || saved === 'dark') theme = saved
  } catch (error) { /* 读不了存储时跟随系统。 */ }
  document.documentElement.setAttribute('data-theme', theme)
  var colors = { light: '#f6f5f0', dark: '#0f1012' }
  var metas = document.querySelectorAll('meta[name="theme-color"]')
  for (var index = 0; index < metas.length; index++) {
    var own = (metas[index].getAttribute('media') || '').indexOf('dark') >= 0 ? 'dark' : 'light'
    metas[index].setAttribute('content', colors[theme === 'system' ? own : theme])
  }
})()
