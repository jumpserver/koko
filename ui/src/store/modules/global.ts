import { defineStore } from 'pinia';
import type { IGlobalState } from '../interface';

export const useGlobalStore = defineStore('global', {
	state: (): IGlobalState => ({
		inited: false,
		i18nLoaded: false,
	}),
	getters: {},
	actions: {
		init() {
			this.inited = true;
		},
		setI18nLoaded(payload: boolean) {
			this.i18nLoaded = payload;
		},
	},
});
