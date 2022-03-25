import zhLocale from 'element-ui/lib/locale/lang/zh-CN'
import enLocale from 'element-ui/lib/locale/lang/en'
import jaLocale from 'element-ui/lib/locale/lang/ja'
import zh from './cn.json'
import en from './en.json'
import ja from './ja.json'

export default {
  cn: {
    ...zhLocale,
    ...zh
  },
  en: {
    ...enLocale,
    ...en
  },
  ja: {
    ...jaLocale,
    ...ja
  }
}
