import { resolve } from 'path';
import { defineConfig } from 'vite';
import { NaiveUiResolver } from 'unplugin-vue-components/resolvers';

import vue from '@vitejs/plugin-vue';
import tailwindcss from 'tailwindcss';
import autoprefixer from 'autoprefixer';
import Components from 'unplugin-vue-components/vite';

const pathResolve = (dir: string): string => {
    return resolve(__dirname, '.', dir);
};

export default defineConfig({
    plugins: [vue(), Components({ dts: true, resolvers: [NaiveUiResolver()] })],
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
        // port: 9530,
        port: 9527,
        proxy: {
            '^/koko/ws/': {
                target: 'http://192.168.200.29:5050',
                ws: true,
                changeOrigin: true
            },
            '^/api/': {
                target: 'http://192.168.200.29:8080',
                ws: true,
                changeOrigin: true
            }
        }
    },
    build: {
        assetsDir: 'assets',
        outDir: 'dist'
    }
});
