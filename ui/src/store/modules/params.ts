import { defineStore } from 'pinia';
import { IParamsState } from '@/types/modules/store.type';
import { SettingConfig } from '@/types/modules/config.type';

export const useParamsStore = defineStore('params', {
  state: (): IParamsState => ({
    shareId: '',
    shareCode: '',
    currentUser: null,
    setting: {}
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
    }
  }
});
