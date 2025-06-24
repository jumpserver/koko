import type { ConfigEnv, UserConfig } from 'vite';
import { resolve } from 'node:path';
import process from 'node:process';
import tailwindcss from '@tailwindcss/vite';
import vue from '@vitejs/plugin-vue';
import vueJsx from '@vitejs/plugin-vue-jsx';

import { NaiveUiResolver } from 'unplugin-vue-components/resolvers';
import Components from 'unplugin-vue-components/vite';
import { defineConfig, loadEnv } from 'vite';
import { manualChunksPlugin } from 'vite-plugin-webpackchunkname';

function pathResolve(dir: string): string {
  return resolve(__dirname, '.', dir);
}

export default defineConfig(({ mode }: ConfigEnv): UserConfig => {
  const root = process.cwd();
  const env = loadEnv(mode, root);

  return {
    plugins: [
      vue(),
      vueJsx(),
      tailwindcss(),
      manualChunksPlugin(),
      Components({ dts: true, resolvers: [NaiveUiResolver()] }),
    ],
    resolve: {
      extensions: ['.js', '.ts', '.tsx', '.vue', '.json'],
      alias: {
        '@': pathResolve('src'),
      },
    },
    base: env.VITE_PUBLIC_PATH,
    server: {
      port: 9530,
      // port: 9527,
      proxy: {
        '^/koko/ws/': {
          target: env.VITE_KOKO_WS_URL,
          ws: true,
          changeOrigin: true,
        },
        '^/api/': {
          target: env.VITE_KOKO_API_URL,
          ws: true,
          changeOrigin: true,
        },
        '^/static/': {
          target: env.VITE_KOKO_STATIC_URL,
          ws: true,
          changeOrigin: true,
        },
      },
    },
    build: {
      assetsDir: 'assets',
      outDir: 'dist',
      terserOptions: {
        compress: {
          drop_console: true,
          drop_debugger: true,
        },
      },
      // 关闭文件计算
      reportCompressedSize: false,
      sourcemap: false,
      minify: false,
      cssCodeSplit: true,
      rollupOptions: {
        output: {
          entryFileNames: `assets/js/[name]-[hash].js`,
          chunkFileNames: `assets/js/[name]-[hash].js`,
          assetFileNames: `assets/[ext]/[name]-[hash].[ext]`,
          manualChunks(id) {
            if (id.includes('node_modules')) {
              // 把 naive-ui 核心模块打包成一个文件
              if (id.includes('naive-ui')) {
                return 'naive-vendor';
              }

              if (id.includes('@xterm/xterm')) {
                return 'xterm-vendor';
              }

              return id.toString().split('node_modules/')[1].split('/')[0].toString();
            }
          },
        },
      },
    },
  };
});
