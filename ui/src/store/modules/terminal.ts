import { defineStore } from 'pinia';

import type { ITerminalConfig, ObjToKeyValArray } from '@/types/modules/store.type';

export const useTerminalStore = defineStore('terminal', {
  state: (): ITerminalConfig => ({
    fontSize: 14,
    themeName: '',
    quickPaste: '0',
    ctrlCAsCtrlZ: '',
    backspaceAsCtrlH: '0',
    lineHeight: 1,
    fontFamily: 'monaco, Consolas, "Lucida Console", monospace',

    enableZmodem: true,
    zmodemStatus: false,

    currentTab: '',

    termSelectionText: ''
  }),
  getters: {
    getConfig: state => state
  },
  actions: {
    setTerminalConfig(...args: ObjToKeyValArray<ITerminalConfig>) {
      this.$patch({ [args[0]]: args[1] });
    }
  }
});
