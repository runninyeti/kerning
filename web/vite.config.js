import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  // Keep light-dark() as is; transpiled, it ignores the theme toggle.
  build: { cssTarget: ['chrome123', 'firefox120', 'safari17.5'] },
  server: { proxy: { '/api': 'http://localhost:8080' } },
})
