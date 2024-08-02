export interface ITerminalProps {
  // 连接 url
  // connectURL: string;

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

interface Announcement {
  CONTENT: string;
  ID: string;
  LINK: string;
  SUBJECT: string;
}

interface Interface {
  favicon: string;
  login_image: string;
  login_title: string;
  logo_index: string;
  logo_logout: string;
}

export interface SettingConfig {
  ANNOUNCEMENT: Announcement;
  ANNOUNCEMENT_ENABLED: boolean;
  INTERFACE: Interface;
  SECURITY_SESSION_SHARE: boolean;
  SECURITY_WATERMARK_ENABLED: boolean;
}
