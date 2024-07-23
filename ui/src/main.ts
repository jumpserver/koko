import { createApp } from 'vue';

import App from './App.vue';
import pinia from './store';
import router from './router';

// 初始化浏览器样式
import 'normalize.css';
// 引入自定义初始化样式
import '@/styles/reset.scss';

const app = createApp(App);

app.use(router);
app.use(pinia);

app.mount('#app');
