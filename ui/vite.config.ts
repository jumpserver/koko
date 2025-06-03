import { fileURLToPath } from 'node:url';
import { defineConfig, loadEnv } from 'vite';
import { dirname, resolve } from 'node:path';

import type { ConfigEnv, UserConfig } from 'vite';

import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react-swc';

export default defineConfig(({ mode }: ConfigEnv): UserConfig => {
  const __filename = fileURLToPath(import.meta.url);
  const __dirname = dirname(__filename);

  const root = process.cwd();
  const env = loadEnv(mode, root);

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        '@': resolve(__dirname, './src')
      },
      extensions: ['.js', '.jsx', '.ts', '.tsx', '.json']
    },
    server: {
      port: 9530,
      proxy: {
        '^/koko/ws/': {
          target: env.VITE_KOKO_WS_URL,
          ws: true,
          changeOrigin: true
        },
        '^/api/': {
          target: env.VITE_KOKO_API_URL,
          changeOrigin: true
        },
        '^/static/': {
          target: env.VITE_KOKO_STATIC_URL,
          changeOrigin: true
        }
      }
    }
  };
});
