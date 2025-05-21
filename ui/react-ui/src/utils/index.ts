import mitt from 'mitt';

import type { Emitter } from 'mitt';
import type { FitAddon } from '@xterm/addon-fit';

const PORT = document.location.port ? `:${document.location.port}` : '';
const SCHEME = document.location.protocol === 'https:' ? 'wss' : 'ws';

const BASE_WS_URL = SCHEME + '://' + document.location.hostname + PORT;
const BASE_URL = document.location.protocol + '//' + document.location.hostname + PORT;

type EmitterEvent = {
  'emit-resize': { fitAddon: FitAddon };
};

export const getConnectionUrl = (type: 'ws' | 'http') => {
  if (type === 'ws') {
    return BASE_WS_URL;
  }

  return BASE_URL;
};

export const getCookie = (key: string) => {
  const nameEQ = key + '=';
  const ca = document.cookie.split(';');

  for (let i = 0; i < ca.length; i++) {
    let c = ca[i];

    while (c.charAt(0) === ' ') c = c.substring(1, c.length);

    if (c.indexOf(nameEQ) === 0) return c.substring(nameEQ.length, c.length);
  }

  return null;
};

export const getLang = () => {
  const storeLang = getCookie('lang');
  const cookieLang = getCookie('django_language');

  const browserLang = navigator.language || (navigator.languages && navigator.languages[0]) || 'zh';

  return storeLang || cookieLang || browserLang || 'zh';
};

export const formatMessage = (id: string, type: string, data: any) => {
  return JSON.stringify({
    id,
    type,
    data
  });
};

export const emitterEvent: Emitter<EmitterEvent> = mitt();
