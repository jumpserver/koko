import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import type { TerminalConfig, ConnectionInfo, ShareInfo } from '@/types/detail.type';

interface DetailStore {
  connection: Partial<ConnectionInfo>;

  terminalConfig: Partial<TerminalConfig>;

  share: Partial<ShareInfo>;

  setConnectionInfo: (info: Partial<ConnectionInfo>) => void;
  setTerminalConfig: (config: Partial<TerminalConfig>) => void;
  setShareInfo: (info: Partial<ShareInfo>) => void;
}

const useDetail = create(
  persist<DetailStore>(
    set => ({
      connection: {
        username: '',
        address: '',
        assetName: '',
        sessionId: ''
      },

      terminalConfig: {
        fontFamily: 'Fira Code',
        fontSize: 14,
        lineHeight: 1,
        cursorBlink: true,
        cursorStyle: 'bar',
        themeName: '',
        quickPaste: '0',
        backspaceAsCtrlH: '0',
        theme: 'Default'
      },

      share: {
        shareCode: '',
        enabledShare: false,
        onlineUsers: [],
        searchEnabledShareUser: []
      },

      setConnectionInfo: (info: Partial<ConnectionInfo>) =>
        set(state => ({ connection: { ...state.connection, ...info } })),

      setTerminalConfig: (config: Partial<TerminalConfig>) =>
        set(state => ({ terminalConfig: { ...state.terminalConfig, ...config } })),

      setShareInfo: (info: Partial<ShareInfo>) => set(state => ({ share: { ...state.share, ...info } }))
    }),
    {
      name: 'KOKO_USER_CUSTOM_CONFIG',
      storage: createJSONStorage(() => localStorage)
    }
  )
);

export default useDetail;
