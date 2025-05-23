import { create } from 'zustand';
import type { TerminalConfig, ConnectionInfo, ShareInfo } from '@/types/detail.type';

interface DetailStore {
  connection: Partial<ConnectionInfo>;

  terminalConfig: Partial<TerminalConfig>;

  share: Partial<ShareInfo>;

  setConnectionInfo: (info: Partial<ConnectionInfo>) => void;
  setTerminalConfig: (config: Partial<TerminalConfig>) => void;
  setShareInfo: (info: Partial<ShareInfo>) => void;
}

const useDetail = create<DetailStore>(set => ({
  connection: {
    username: '',
    address: '',
    assetName: '',
    sessionId: ''
  },

  terminalConfig: {
    fontFamily: 'monospace',
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
    enabledShare: false
  },

  setConnectionInfo: (info: Partial<ConnectionInfo>) =>
    set(state => ({ connection: { ...state.connection, ...info } })),

  setTerminalConfig: (config: Partial<TerminalConfig>) =>
    set(state => ({ terminalConfig: { ...state.terminalConfig, ...config } })),

  setShareInfo: (info: Partial<ShareInfo>) => set(state => ({ share: { ...state.share, ...info } }))
}));

export default useDetail;
