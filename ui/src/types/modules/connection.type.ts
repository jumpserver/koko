import type { Terminal } from '@xterm/xterm';

import type { OnlineUser, ShareUserOptions } from './user.type';

export interface ConnectionState {
  origin: string;

  lunaId: string;

  account: string;

  asset: string;

  protocol: string;

  user: string;

  date_start: string;

  date_end: string;

  shareId: string;

  shareCode: string;

  sessionId: string;

  terminalId: string;

  enableShare: boolean;

  terminal: Terminal;

  socket: WebSocket | null;

  userOptions: ShareUserOptions[];

  onlineUsers: OnlineUser[];

  drawerOpenState: boolean;

  drawerTabIndex: number;
}

export type ContentType = 'setting' | 'file-manager' | '';
