import i18n from 'i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    fallbackLng: 'en',
    interpolation: {
      escapeValue: false
    },
    resources: {
      en: {
        translation: {}
      },
      zh: {
        translation: {}
      },
      ja: {
        translation: {}
      },
      'zh-hans': {
        translation: {}
      }
    }
  });

export default i18n;
