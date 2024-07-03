// i18n.js
import Vue from 'vue'
import axios from 'axios'
import store from '../../store'
import locale from 'element-ui/lib/locale'
import VueI18n from 'vue-i18n'
import messages from './langs'
import date from './date'
import VueCookies from 'vue-cookies'
import {BASE_URL} from "@/utils/common";

Vue.use(VueI18n)

const cookieLang = VueCookies.get('django_language')
const storeLang = VueCookies.get('lang')
const browserLang = navigator.systemLanguage || navigator.language
let lang = cookieLang || storeLang || browserLang || 'zh'
lang = lang.slice(0, 2)

const i18n = new VueI18n({
  locale: lang,
  fallbackLocale: 'en',
  silentFallbackWarn: true,
  silentTranslationWarn: true,
  dateTimeFormats: date,
  messages
})
locale.i18n((key, value) => i18n.t(key, value)) // 重点: 为了实现element插件的多语言切换



axios.get(`${BASE_URL}/api/v1/settings/i18n/koko/?lang=${lang}&flat=0`)
    .then((res) => {
      if (res.status !== 200) {
        return
      }
      const data = res.data
      for (const key in data) {
        // eslint-disable-next-line no-prototype-builtins
        if (data.hasOwnProperty(key)) {
          i18n.mergeLocaleMessage(key, data[key])
        }
      }
    })
    .finally(() => {
      store.dispatch('setI18nLoaded', true)
    })

export default i18n
