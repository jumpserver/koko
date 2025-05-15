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
