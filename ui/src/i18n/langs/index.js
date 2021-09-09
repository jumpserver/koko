import zhLocale from 'element-ui/lib/locale/lang/zh-CN'
import enLocale from 'element-ui/lib/locale/lang/en'
import zh from './cn.json'
import en from './en.json'

export default {
  cn: {
    ...zhLocale,
    ...zh
  },
  en: {
    ...enLocale,
    ...en
  }
}
