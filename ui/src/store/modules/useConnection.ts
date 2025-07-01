// 连接状态管理 - 每个 iframe 只管理一个连接
import { defineStore } from 'pinia';

import type { ConnectionState } from '@/types/modules/connection.type';

export const useConnectionStore = defineStore('connection', {
  state: (): Partial<ConnectionState> => ({
    origin: '',
    lunaId: '',
    shareId: '',
    shareCode: '',
    sessionId: '',
    terminalId: '',
    enableShare: false,
    terminal: undefined,
    socket: null,
    userOptions: [],
    onlineUsers: [],
    drawerOpenState: false,
    drawerTabIndex: 0,
  }),
  getters: {
    isConnected: state => !!state.socket && state.socket.readyState === WebSocket.OPEN,
    hasShare: state => !!state.shareId && !!state.shareCode,
  },
  actions: {
    setConnectionState(connectionState: Partial<ConnectionState>) {
      Object.assign(this, connectionState);
    },
    updateConnectionState(connectionState: Partial<ConnectionState>) {
      Object.assign(this, connectionState);
    },
    resetConnectionState() {
      this.$reset();
    },
  },
});
