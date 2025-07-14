import type { LUNA_MESSAGE_TYPE } from './message.type';

export interface LunaMessageEvents {
  [LUNA_MESSAGE_TYPE.PING]: {
    data: string;
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
    data: string;
  };
  [LUNA_MESSAGE_TYPE.GET_FILE_CONNECT_TOKEN]: {
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
    data: ShareUserRequest;
  };
  [LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE]: {
    data: string;
  };
  [LUNA_MESSAGE_TYPE.CLOSE]: {
    data: string;
  };
  [LUNA_MESSAGE_TYPE.CONNECT]: {
    data: LunaMessage;
  };
  [LUNA_MESSAGE_TYPE.TERMINAL_ERROR]: {
    data: LunaMessage;
  };
  [LUNA_MESSAGE_TYPE.MESSAGE_NOTIFY]: {
    data: LunaMessage;
  };
  [LUNA_MESSAGE_TYPE.KEYEVENT]: {
    data: string;
  };
  [LUNA_MESSAGE_TYPE.TERMINAL_CONTENT]: {
    data: LunaMessage;
  };
  [LUNA_MESSAGE_TYPE.TERMINAL_CONTENT_RESPONSE]: {
    data: TerminalContentRepsonse;
  };
  [LUNA_MESSAGE_TYPE.CLICK]: {
    data: string;
  };
  [LUNA_MESSAGE_TYPE.FILE_MANAGE_EXPIRED]: {
    data: string;
  };
  [LUNA_MESSAGE_TYPE.CHANGE_MAIN_THEME]: {
    data: string;
  };
  [LUNA_MESSAGE_TYPE.OPEN_K8S_SETTING]: {
    data: string;
  };
}

export interface LunaMessage {
  id: string;
  name: string;
  origin: string;
  category: string;
  data: string | object | null;
  theme?: string;
  user_meta?: string;
  token?: string;
}

export interface ShareUserRequest {
  name: string;
  data: {
    sessionId: string;
    requestData: {
      expired_time: number;
      action_permission: string;
      action_perm: string;
      users: string[];
    };
  };
}

export interface ShareUserResponse {
  shareId: string;
  code: string;
  terminalId: string;
}

export interface TerminalSessionInfo {
  session: TerminalSession;
  permission: TerminalPermission;
  backspaceAsCtrlH: boolean;
  ctrlCAsCtrlZ: boolean;
  themeName: string;
}

export interface TerminalSession {
  id: string;
  user: string;

  userId: string;
}

export interface TerminalPermission {
  actions: string[];
}

export interface TerminalContentRepsonse {
  terminalId: string;
  content: string;
  sessionId: string;
}
