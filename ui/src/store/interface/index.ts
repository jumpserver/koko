import { customTreeOption, SettingConfig } from '@/hooks/interface';

export interface IGlobalState {
    initialized: boolean;

    i18nLoaded: boolean;
}

export interface IParamsState {
    shareId: string;

    shareCode: string;

    currentUser: any;

    setting: SettingConfig;
}

export interface ITerminalConfig {
    // 主题
    themeName: string;

    // 快速粘贴
    quickPaste: string;

    // Ctrl
    ctrlCAsCtrlZ: string;

    // 退格键
    backspaceAsCtrlH: string;

    // 字体大小
    fontSize: number;

    // 行高
    lineHeight: number;

    // 字体
    fontFamily: string;

    // 是否开启 Zmodem
    enableZmodem: boolean;

    // 当前 Zmodem 状态
    zmodemStatus: boolean;

    // 当前页签
    currentTab: string;
}

export interface ITreeState {
    connectInfo: any;

    treeNodes: customTreeOption[];

    currentNode: customTreeOption;
}

export type ObjToKeyValArray<T> = {
    [K in keyof T]: [K, T[K]];
}[keyof T];
