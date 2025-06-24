export interface ITerminalSettings {
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

  // 主题
  theme: string;
}
