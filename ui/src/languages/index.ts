import { message } from './modules';
import { createI18n } from 'vue-i18n';

export const i18n = createI18n({
	allowComposition: true,
	legacy: false,
	messages: message,
});
