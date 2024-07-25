import { useCookies } from 'vue3-cookies';

const PORT = document.location.port ? `:${document.location.port}` : '';
const SCHEME = document.location.protocol === 'https' ? 'wss' : 'ws';

export const BASE_WS_URL = SCHEME + '://' + document.location.hostname + PORT;
export const BASE_URL = document.location.protocol + '//' + document.location.hostname + PORT;

const { cookies } = useCookies();

const storeLang = cookies.get('lang');
const cookieLang = cookies.get('django_language');

const browserLang = navigator.language || (navigator.languages && navigator.languages[0]) || 'zh';

export const lang = (cookieLang || storeLang || browserLang || 'zh').slice(0, 2);
