import Vue from 'vue'
import VueRouter from 'vue-router'
import VueLogger from 'vuejs-logger'
import '@/guards' // permission control
import App from './App.vue'
import router from './router'
import i18n from './i18n/i18n'
import loggerOptions from './plugins/logger'
import store from '../store'
import ElementUI from 'element-ui'
import 'element-ui/lib/theme-chalk/index.css';
import 'element-ui/lib/theme-chalk/display.css';
import '@/styles/index.css' // global css
import contextmenu from "v-contextmenu";
import "v-contextmenu/dist/index.css";
import VueCookies from 'vue-cookies'

import { library } from '@fortawesome/fontawesome-svg-core'
import { FontAwesomeIcon } from '@fortawesome/vue-fontawesome'
import {
  faUser, faRightFromBracket,
  faKeyboard,faTrashCan, faFilePen,faFile,
    faFileCircleMinus,faEye
} from '@fortawesome/free-solid-svg-icons'

library.add(
    faUser, faRightFromBracket,
    faKeyboard,faTrashCan, faFilePen,faFile,
    faFileCircleMinus,
    faEye,
)
Vue.component('font-awesome-icon', FontAwesomeIcon)

Vue.use(VueCookies);
Vue.config.productionTip = false
Vue.use(VueRouter)
Vue.use(VueLogger, loggerOptions)
Vue.use(ElementUI)
Vue.use(contextmenu);

new Vue({
  router,
  i18n,
  store,
  render: h => h(App),
}).$mount('#app')
