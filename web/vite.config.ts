import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const backend = 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [react()],
  // /feed 是前缀匹配，也覆盖 /feed.xml。
  server: { proxy: { '/api': backend, '/feed': backend, '/sources.opml': backend } },
})
