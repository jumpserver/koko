// store/index.js
import Vue from 'vue';
import Vuex from 'vuex';

Vue.use(Vuex);

const store = new Vuex.Store({
    state: {
        inited: false,
        i18nLoaded: false,
    },
    mutations: {
        SET_INIT: (state, value) => {
            state.inited = value
        },
        SET_I18N_LOADED(state, payload) {
            state.i18nLoaded = payload;
        },
    },
    actions: {
        init({ commit }) {
            commit('SET_INIT', true)
        },
        setI18nLoaded({ commit }, payload) {
            commit('SET_I18N_LOADED', payload);
        },
    },
});

export default store;
