import { defineStore } from 'pinia';

import type { ObjToKeyValArray } from '@/types';
import type { ITerminalSettings } from '@/types/modules/terminal.type';

export const useTerminalSettingsStore = defineStore('terminalSettings', {
  state: (): Partial<ITerminalSettings> => ({
    fontSize: 14,
    lineHeight: 1,
    fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
    themeName: '',
    quickPaste: '0',
    ctrlCAsCtrlZ: '0',
    backspaceAsCtrlH: '0',
    theme: '',
  }),
  getters: {
    getConfig: state => state,
  },
  actions: {
    setDefaultTerminalConfig(...args: ObjToKeyValArray<ITerminalSettings>) {
      this.$patch({ [args[0]]: args[1] });
    },
  },
});
