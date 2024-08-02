export interface IGlobalState {
  initialized: boolean;

  i18nLoaded: boolean;
}

export interface ITerminalConfig {
  // 主题
  themeName: string;

  // 快速粘贴
  quickPaste: string;

  // Ctrl
  ctrlCAsCtrlZ: string;

  // 退格键
  backspaceAsCtrlH: string;

  // 字体大小
  fontSize: number;

  // 行高
  lineHeight: number;
}

export type ObjToKeyValArray<T> = {
  [K in keyof T]: [K, T[K]];
}[keyof T];
