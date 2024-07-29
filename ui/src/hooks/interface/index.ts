export interface ITerminalProps {
  // 连接 url
  connectURL: string;

  //
  shareCode?: string;

  // 主题名称
  themeName?: string;

  //
  enableZmodem: boolean;
}

export interface ILunaConfig {
  fontSize?: number;

  quickPaste?: string;

  backspaceAsCtrlH?: string;

  ctrlCAsCtrlZ?: string;

  lineHeight?: number;
}
