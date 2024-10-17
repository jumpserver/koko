import { defineStore } from 'pinia';
import { SettingConfig } from '@/hooks/interface';

export interface IKubernetesState {
    // 全局的唯一 TerminalId
    globalTerminalId: string;

    globalSetting: SettingConfig;
}

export const useKubernetesStore = defineStore('kubernetes', {
    state: (): IKubernetesState => {
        return {
            globalTerminalId: '',
            setting: {}
        };
    },
    actions: {
        setGlobalTerminalId(id: string) {
            this.globalTerminalId = id;
        },
        setGlobalSetting(setting: SettingConfig) {
            this.globalSetting = setting;
        }
    }
});
