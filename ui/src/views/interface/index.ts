import { Component } from 'vue';

export interface ISettingProp {
  title: string;
  icon: Component;
  disabled: () => any;
  click: () => any;
  content?: any;
}
