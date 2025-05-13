import { TreeOption } from 'naive-ui';

interface Interface {
  favicon: string;
  login_image: string;
  login_title: string;
  logo_index: string;
  logo_logout: string;
}

interface Announcement {
  CONTENT: string;
  ID: string;
  LINK: string;
  SUBJECT: string;
}

export interface ILunaConfig {
  fontSize?: number;

  quickPaste?: string;

  backspaceAsCtrlH?: string;

  ctrlCAsCtrlZ?: string;

  lineHeight?: number;

  fontFamily: string;
}

export interface SettingConfig {
  ANNOUNCEMENT?: Announcement;
  ANNOUNCEMENT_ENABLED?: boolean;
  INTERFACE?: Interface;
  SECURITY_SESSION_SHARE?: boolean;
  SECURITY_WATERMARK_ENABLED?: boolean;
  SECURITY_WATERMARK_SESSION_CONTENT?: string;
  SECURITY_WATERMARK_WIDTH?: number;
  SECURITY_WATERMARK_HEIGHT?: number;
  SECURITY_WATERMARK_ROTATE?: number;
  SECURITY_WATERMARK_FONT_SIZE?: number;
  SECURITY_WATERMARK_COLOR?: string;
}

export interface ITerminalProps {
  // 主题名称
  themeName?: string;

  terminalType: string;

  socket?: WebSocket;

  indexKey?: string;
}

export interface customTreeOption extends TreeOption {
  id?: string;

  k8s_id?: string;

  namespace?: string;

  pod?: string;

  container?: string;
}
