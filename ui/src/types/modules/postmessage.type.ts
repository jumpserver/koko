

import { LUNA_MESSAGE_TYPE } from "./message.type";



export interface LunaMessageEvents {
    [LUNA_MESSAGE_TYPE.PING]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.PONG]: {
        data: string;
    };
    [LUNA_MESSAGE_TYPE.CMD]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.FOCUS]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.OPEN]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.FILE]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.CREATE_FILE_CONNECT_TOKEN]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.SESSION_INFO]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.SHARE_USER]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.SHARE_USER_REMOVE]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.SHARE_USER_ADD]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.TERMINAL_THEME_CHANGE]: {
        data: LunaMessage;
    };

    [LUNA_MESSAGE_TYPE.SHARE_CODE_REQUEST]: {
        data: LunaMessage;
    };
    [LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE]: {
        data: LunaMessage;
    };

}


export interface LunaMessage {
    id: string;
    name: string
    origin: string;
    protocol: string;
    data: string | object | null;
}
