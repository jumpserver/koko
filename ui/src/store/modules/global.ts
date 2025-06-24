import type { IGlobalState } from '@/types/modules/store.type';
import { defineStore } from 'pinia';

export const useGlobalStore = defineStore('global', {
  state: (): IGlobalState => ({
    initialized: false,
    i18nLoaded: false,
  }),
  getters: {},
  actions: {
    setI18nLoaded(payload: boolean) {
      this.i18nLoaded = payload;
    },
  },
});
