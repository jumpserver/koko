import { defineStore } from 'pinia';
import type { ITheme } from '@xterm/xterm';
import type { ObjToKeyValArray } from '@/types';

export interface ITerminalSettings {
  // 终端字体大小
  fontSize: number;

  // 终端行高
  lineHeight: number;

  // 终端字体
  fontFamily: string;

  // 终端主题
  themeName: string;

  // 是否启用 Ctrl+C 作为 Ctrl+Z
  ctrlCAsCtrlZ: string;

  // 是否启用快速粘贴
  quickPaste: string;

  // 是否启用退格键作为 Ctrl+H
  backspaceAsCtrlH: string;

  // 主题
  theme: string;
}

export const useTerminalSettingsStore = defineStore('terminalSettings', {
  state: (): Partial<ITerminalSettings> => ({
    fontSize: 14,
    lineHeight: 1,
    fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
    themeName: '',
    quickPaste: '0',
    ctrlCAsCtrlZ: '0',
    backspaceAsCtrlH: '0',
    theme: ''
  }),
  getters: {
    getConfig: state => state
  },
  actions: {
    setDefaultTerminalConfig(...args: ObjToKeyValArray<ITerminalSettings>) {
      this.$patch({ [args[0]]: args[1] });
    }
  }
});
