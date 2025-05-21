import { create } from 'zustand';

interface TerminalSetting {
  // 终端字体大小
  fontSize: number;

  // 终端行高
  lineHeight: number;

  // 终端字体
  fontFamily: string;

  // 终端主题
  themeName: string;

  // 是否启用 Ctrl+C 作为 Ctrl+Z
  ctrlCAsCtrlZ: string;

  // 是否启用快速粘贴
  quickPaste: string;

  // 是否启用退格键作为 Ctrl+H
  backspaceAsCtrlH: string;

  cursorInactiveStyle: 'bar';

  // 主题
  theme: string;

  setDefaultTerminalConfig: (...args: ObjToKeyValArray<Omit<TerminalSetting, 'setDefaultTerminalConfig'>>) => void;
}

type ObjToKeyValArray<T> = {
  [K in keyof T]: [K, T[K]];
}[keyof T];

const useTerminalSetting = create<TerminalSetting>((set, get) => ({
  fontSize: 14,
  lineHeight: 1,
  fontFamily: 'Maple Mono, monaco, Consolas, "Lucida Console", monospace',
  themeName: '',
  cursorInactiveStyle: 'bar',
  quickPaste: '0',
  ctrlCAsCtrlZ: '0',
  backspaceAsCtrlH: '0',
  theme: '',
  setDefaultTerminalConfig: (...args: ObjToKeyValArray<Omit<TerminalSetting, 'setDefaultTerminalConfig'>>) => {
    set({ [args[0]]: args[1] });
  }
}));

export default useTerminalSetting;
