import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  // Relative paths, so the static build works under a GitHub Pages subpath.
  base: './',
  // Keep light-dark() as is; transpiled, it ignores the theme toggle.
  build: { cssTarget: ['chrome123', 'firefox120', 'safari17.5'] },
  server: { proxy: { '/api': 'http://localhost:8080' } },
})
