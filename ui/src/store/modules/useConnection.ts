// 构建 terminalId、socket、lunaId、origin、currentShareId、currentShareCode、currentEnableShare、currentUserOptions、currentOnlineUsers 等的 Map 关系
import { defineStore } from 'pinia';
import type { ConnectionState } from '@/types/modules/connection.type';

type terminalId = string

export const useConnectionStore = defineStore('connection', {
  state: () => ({
    connectionStateMap: new Map<terminalId, Partial<ConnectionState>>()
  }),
  actions: {
    setConnectionState(terminalId: terminalId, connectionState: Partial<ConnectionState>) {
      this.connectionStateMap.set(terminalId, connectionState);
    },
    getConnectionState(terminalId: terminalId) {
      return this.connectionStateMap.get(terminalId);
    },
    deleteConnectionState(terminalId: terminalId) {
      this.connectionStateMap.delete(terminalId);
    },
    updateConnectionState(terminalId: terminalId, connectionState: Partial<ConnectionState>) {
      const state = this.connectionStateMap.get(terminalId);

      if (state) {
        Object.assign(state, connectionState);
      }
    }
  }
});
