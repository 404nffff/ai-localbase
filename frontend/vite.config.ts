import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    manifest: true,
    rollupOptions: {
      output: {
        // 独立缓存基础框架与按需解析器，图表依赖只经动态入口载入。
        manualChunks(id) {
          if (/node_modules\/(react|react-dom|scheduler)\//.test(id)) return 'react'
          if (/node_modules\/(react-markdown|remark-gfm)\//.test(id)) return 'markdown'
          if (id.includes('/node_modules/cytoscape/')) return 'diagram-cytoscape'
          if (id.includes('/node_modules/cytoscape-fcose/')) return 'diagram-fcose'
          if (id.includes('/node_modules/cytoscape-cose-bilkent/')) return 'diagram-cose'
        },
      },
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/v1': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/upload': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/livez': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/readyz': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    }
  }
})