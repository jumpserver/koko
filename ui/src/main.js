import Vue from 'vue'
import VueRouter from 'vue-router'
import VueLogger from 'vuejs-logger'
import App from './App.vue'
import router from './router'
import i18n from './i18n/i18n'
import loggerOptions from './plugins/logger'
import ElementUI from 'element-ui'
import 'element-ui/lib/theme-chalk/index.css';
import 'element-ui/lib/theme-chalk/display.css';
import contextmenu from "v-contextmenu";
import "v-contextmenu/dist/index.css";
import VueCookies from 'vue-cookies'
Vue.use(VueCookies);
Vue.config.productionTip = false
Vue.use(VueRouter)
Vue.use(VueLogger, loggerOptions)
Vue.use(ElementUI)
Vue.use(contextmenu);

// 同源策略，方便与父组件事件通信
const domain = document.domain.split('.').slice(-2).join('.');
let isDomain = /^(\w+)\.([A-Za-z]+)$/.test(domain);
if (isDomain) {
  document.domain = domain;
}

new Vue({
  router,
  i18n,
  render: h => h(App),
}).$mount('#app')
