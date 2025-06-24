import type { SettingConfig } from '@/types/modules/config.type';
import type { IParamsState } from '@/types/modules/store.type';
import { defineStore } from 'pinia';

export const useParamsStore = defineStore('params', {
  state: (): IParamsState => ({
    shareId: '',
    shareCode: '',
    currentUser: null,
    setting: {},
  }),
  actions: {
    setShareId(shareId: string) {
      this.shareId = shareId;
    },
    setShareCode(shareCode: string) {
      this.shareCode = shareCode;
    },
    setCurrentUser(curremtUser: any) {
      this.currentUser = curremtUser;
    },
    setSetting(setting: SettingConfig) {
      this.setting = setting;
    },
  },
});
