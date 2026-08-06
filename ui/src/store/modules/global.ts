import { defineStore } from 'pinia';

import type { IGlobalState } from '@/types/modules/store.type';

export const useGlobalStore = defineStore('global', {
  state: (): IGlobalState => ({
    initialized: false,
    i18nLoaded: false,
    interfaceVendor: null,
  }),
  getters: {},
  actions: {
    setI18nLoaded(payload: boolean) {
      this.i18nLoaded = payload;
    },
    setInterfaceVendor(payload: string | null) {
      this.interfaceVendor = payload;
    },
  },
});
