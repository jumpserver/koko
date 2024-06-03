import zhLocale from 'element-ui/lib/locale/lang/zh-CN'
import zhTWLocale from 'element-ui/lib/locale/lang/zh-TW'
import enLocale from 'element-ui/lib/locale/lang/en'
import jaLocale from 'element-ui/lib/locale/lang/ja'
import zh from './zh.json'
import zhHant from './zh_Hant.json'
import en from './en.json'
import ja from './ja.json'

const messages = {
    zh: {
        ...zhLocale,
        ...zh
    },
    zh_hant: {
        ...zhTWLocale,
        ...zhHant
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

export default messages