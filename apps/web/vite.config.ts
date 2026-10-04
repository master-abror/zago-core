import { svelte } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vitest/config';

// Same-origin saat pengembangan: browser hanya bicara ke :5173; Vite meneruskan
// /api dan /ws (dengan upgrade WebSocket) ke API (docs/15 §6).
const target = process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [svelte()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': { target, changeOrigin: false },
      '/ws': { target, ws: true, changeOrigin: false },
    },
  },
  test: {
    // Default 'node': tes logika (core/api, can(), registry) tak butuh DOM, dan memuat jsdom dari
    // disk lambat (mis. /mnt/c di WSL) memakan hampir seluruh waktu tes sampai worker timeout.
    // Tes komponen memilih DOM per berkas dengan komentar pertama: // @vitest-environment jsdom
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
});
