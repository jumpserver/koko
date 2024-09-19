import { TreeOption } from 'naive-ui';
import type { Component, Ref } from 'vue';
import { Terminal } from '@xterm/xterm';

export interface ITerminalProps {
    // 主题名称
    themeName?: string;

    terminalType: string;

    socket?: WebSocket;

    indexKey?: string;
}

export interface ILunaConfig {
    fontSize?: number;

    quickPaste?: string;

    backspaceAsCtrlH?: string;

    ctrlCAsCtrlZ?: string;

    lineHeight?: number;

    fontFamily: string;
}

interface Announcement {
    CONTENT: string;
    ID: string;
    LINK: string;
    SUBJECT: string;
}

interface Interface {
    favicon: string;
    login_image: string;
    login_title: string;
    logo_index: string;
    logo_logout: string;
}

export interface SettingConfig {
    ANNOUNCEMENT?: Announcement;
    ANNOUNCEMENT_ENABLED?: boolean;
    INTERFACE?: Interface;
    SECURITY_SESSION_SHARE?: boolean;
    SECURITY_WATERMARK_ENABLED?: boolean;
}

export interface customTreeOption extends TreeOption {
    id?: string;

    k8s_id?: string;

    namespace?: string;

    pod?: string;

    container?: string;
}

export interface EmitEvent<E = string, D = any> {
    event: E;
    data: D;
}

export interface paramsOptions {
    enableZmodem: boolean;

    zmodemStatus: Ref<boolean>;

    emitCallback?: (type: string, msg: any, terminal: Terminal) => void;

    i18nCallBack?: (key: string) => string;

    isK8s?: boolean;
}

export interface IContainer {
    name: string;

    type: string;

    pod?: string;

    container?: string;

    namespace?: string;
}

export interface IPods {
    name: string;

    type: string;

    containers?: IContainer[];

    children?: IContainer[];

    namespace?: string;

    container?: string;

    prefix: Component;
}
