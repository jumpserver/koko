import { resolve } from 'path';
import { defineConfig } from 'vite';
import { createSvgIconsPlugin } from 'vite-plugin-svg-icons';
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
    plugins: [
        vue(),
        manualChunksPlugin(),
        createSvgIconsPlugin({
            iconDirs: [resolve(process.cwd(), 'src/assets/icons')],
            symbolId: 'icon-[dir]-[name]'
        }),
        Components({ dts: true, resolvers: [NaiveUiResolver()] })
    ],
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
                target: 'localhost:5050',
                ws: true,
                changeOrigin: true
            },
            '^/api/': {
                target: 'localhost:8080',
                ws: true,
                changeOrigin: true
            },
            '^/static/': {
                target: 'localhost:8080',
                ws: true,
                changeOrigin: true
            }
        }
    }
});
