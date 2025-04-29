import type { Terminal } from '@xterm/xterm';
import type { ShareUserOptions, OnlineUser } from './user.type';

export interface ConnectionState {
  origin: string;

  lunaId: string;

  shareId: string;

  shareCode: string;

  sessionId: string;

  terminalId: string;

  enableShare: boolean;
  
  terminal: Terminal;

  socket: WebSocket | null;

  userOptions: ShareUserOptions[];

  onlineUsers: OnlineUser[];
}

export type ContentType = 'setting' | 'file-manager';
