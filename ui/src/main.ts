import { createApp } from 'vue';

import App from './App.vue';
import pinia from './store';
import router from './router';
import i18n from './languages';
import VueCookies from 'vue3-cookies';

import './index.css';
// 初始化浏览器样式
import 'normalize.css';
// 引入自定义初始化样式
import '@/style/reset.scss';
// 引入图标注册脚本
import 'virtual:svg-icons-register';
// 引入 xterm 样式
import '@xterm/xterm/css/xterm.css';

const app = createApp(App);

app.use(i18n);
app.use(pinia);
app.use(router);
app.use(VueCookies);

app.mount('#app');
