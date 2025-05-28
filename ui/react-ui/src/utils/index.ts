import mitt from 'mitt';

import type { Emitter } from 'mitt';

import { ASCII_DEL, ASCII_BACKSPACE } from '@/config';

const PORT = document.location.port ? `:${document.location.port}` : '';
const SCHEME = document.location.protocol === 'https:' ? 'wss' : 'ws';

const BASE_WS_URL = SCHEME + '://' + document.location.hostname + PORT;
const BASE_URL = document.location.protocol + '//' + document.location.hostname + PORT;

type EmitterEvent = {
  'emit-resize': void;
  'emit-generate-file-token': void;
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

export const preprocessInput = (data: string, backspaceAsCtrlH: string) => {
  // 如果两个条件都满足，则将输入字符从 DELETE（ASCII 127）转换为 BACKSPACE（ASCII 8，等同于 Ctrl+H）
  if (backspaceAsCtrlH === '1' && data.charCodeAt(0) === ASCII_DEL) {
    data = String.fromCharCode(ASCII_BACKSPACE);

    return data;
  }

  return data;
};

export const updateIcon = (faviconURL: string) => {
  let link = document.querySelector("link[rel*='icon']") as HTMLLinkElement;

  if (!link) {
    link = document.createElement('link') as HTMLLinkElement;
    link.type = 'image/x-icon';
    link.rel = 'shortcut icon';
    document.getElementsByTagName('head')[0].appendChild(link);

    return (link.href = faviconURL);
  }

  link.href = faviconURL;
};

export const sendEventToLuna = (name: string, data: any, lunaId: string | null = '', origin: string | null = '*') => {
  if (lunaId !== null && origin !== null) {
    const targetOrigin = origin === '' ? '*' : origin;
    window.parent.postMessage({ name, id: lunaId, data }, targetOrigin);
  }
};

export const emitterEvent: Emitter<EmitterEvent> = mitt();
