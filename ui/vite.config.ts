import { resolve } from 'path';
import { defineConfig } from 'vite';
import { NaiveUiResolver } from 'unplugin-vue-components/resolvers';
import { manualChunksPlugin } from 'vite-plugin-webpackchunkname';

import vue from '@vitejs/plugin-vue';
import tailwindcss from 'tailwindcss';
import autoprefixer from 'autoprefixer';
import Components from 'unplugin-vue-components/vite';

const pathResolve = (dir: string): string => {
  return resolve(__dirname, '.', dir);
};

export default defineConfig({
  plugins: [vue(), manualChunksPlugin(), Components({ dts: true, resolvers: [NaiveUiResolver()] })],
  resolve: {
    extensions: ['.js', '.ts', '.vue', '.json'],
    alias: {
      '@': pathResolve('src')
    }
  },
  css: {
    postcss: {
      plugins: [tailwindcss, autoprefixer]
    }
  },
  base: '/koko/',
  server: {
    port: 9530,
    // port: 9527,
    proxy: {
      '^/koko/ws/': {
        target: 'http://192.168.200.34:5050',
        ws: true,
        changeOrigin: true
      },
      '^/api/': {
        target: 'http://192.168.200.34:8080',
        ws: true,
        changeOrigin: true
      }
    }
  },
  build: {
    assetsDir: 'assets',
    outDir: 'dist',
    terserOptions: {
      compress: {
        drop_console: true,
        drop_debugger: true
      }
    },
    // 关闭文件计算
    reportCompressedSize: false,
    sourcemap: false,
    minify: false,
    cssCodeSplit: true,
    rollupOptions: {
      external: ['vue'],
      output: {
        globals: {
          vue: 'Vue'
        },
        entryFileNames: `assets/js/[name]-[hash].js`,
        chunkFileNames: `assets/js/[name]-[hash].js`,
        assetFileNames: `assets/[ext]/[name]-[hash].[ext]`,
        manualChunks(id) {
          if (id.includes('node_modules')) {
            //把 vue vue-router 核心模块打包成一个文件
            if (id.includes('vue')) {
              return 'vue';
            } else {
              //最小化拆分包
              return id.toString().split('node_modules/')[1].split('/')[0].toString();
            }
          }
        }
      }
    }
  }
});
