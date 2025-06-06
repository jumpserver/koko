

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
}


export interface LunaMessage {
    id: string;
    name: string
    origin: string;
    protocol: string;
    data: string | object | null;
}
