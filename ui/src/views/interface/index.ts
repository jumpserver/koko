import { Component } from 'vue';
import { Composer } from 'vue-i18n';

export type TranslateFunction = Composer['t'];

export interface ISettingProp {
  title: string;
  icon: Component;
  disabled: () => any;
  click: () => any;
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
