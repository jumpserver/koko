import { Component, FunctionalComponent } from 'vue';
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

export interface ShareUserOptions {
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

export interface ISettingConfig {
  theme?: string;
  drawerTitle: string;

  items: Array<{
    type: 'select' | 'button' | 'create' | 'list';
    label: string;
    labelIcon: FunctionalComponent;
    labelStyle: {
      fontSize: string;
    };
    showMore?: boolean;
    disabled?: boolean;
    value?: string;
    options?: any;
  }>;
}
