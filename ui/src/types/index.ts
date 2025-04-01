import { Component } from 'vue';
import { Composer } from 'vue-i18n';

export type TranslateFunction = Composer['t'];

export type ObjToKeyValArray<T> = {
  [K in keyof T]: [K, T[K]];
}[keyof T];

export interface ISettingProp {
  label: string;
  title: string;
  icon: Component;
  disabled: () => any;
  click: (user: any) => any;
  content?: any;
}

export interface shareUser {
  id: string;

  name: string;

  username: string;
}

export interface IXtermTheme {
  background: string;
  black: string;
  blue: string;
  brightBlack: string;
  brightBlue: string;
  brightCyan: string;
  brightGreen: string;
  brightMagenta: string;
  brightRed: string;
  brightWhite: string;
  brightYellow: string;
  cursor: string;
  cyan: string;
  foreground: string;
  green: string;
  magenta: string;
  red: string;
  white: string;
  yellow: string;
}
