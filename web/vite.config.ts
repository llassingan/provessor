import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig(() => {
  const apiTarget = process.env.VITE_API_TARGET ?? 'http://localhost:10000';

  return {
    plugins: [react(), tailwindcss()],
    server: {
      host: '0.0.0.0',
      port: 10001,
      allowedHosts: ['.llassingan.web.id'],
      proxy: {
        '/api': apiTarget,
      },
    },
  };
});
