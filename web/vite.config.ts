import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev, the Go backend runs on :8080 and Vite serves the UI on :5173.
// Proxy the API and WebSocket through so the app calls same-origin paths.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
  build: { outDir: 'dist', sourcemap: false },
})
