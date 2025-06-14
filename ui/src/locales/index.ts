import { LanguageCode } from '@/utils/config';
import { message } from './modules';
import { createI18n } from 'vue-i18n';

import date from './date';

const i18n = createI18n({
  locale: LanguageCode,
  fallbackLocale: 'en',
  legacy: false,
  allowComposition: true,
  silentFallbackWarn: true,
  silentTranslationWarn: true,
  messages: message,
  dateTimeFormats: date
});

export default i18n;
