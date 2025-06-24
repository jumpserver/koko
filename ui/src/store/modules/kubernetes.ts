import type { SettingConfig } from '@/types/modules/config.type';
import { defineStore } from 'pinia';

export interface IKubernetesState {
  // 全局的唯一 TerminalId
  globalTerminalId: string;

  globalSetting: SettingConfig;

  lastReceiveTime: any;

  lastSendTime: any;
}

export const useKubernetesStore = defineStore('kubernetes', {
  state: (): IKubernetesState => {
    return {
      globalTerminalId: '',
      globalSetting: {},
      lastReceiveTime: new Date(),
      lastSendTime: new Date(),
    };
  },
  actions: {
    setGlobalTerminalId(id: string) {
      this.globalTerminalId = id;
    },
    setGlobalSetting(setting: SettingConfig) {
      this.globalSetting = setting;
    },
    setLastReceiveTime(time: any) {
      this.lastReceiveTime = time;
    },
    setLastSendTime(time: any) {
      this.lastSendTime = time;
    },
  },
});
