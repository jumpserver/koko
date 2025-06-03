interface Announcement {
  CONTENT: string;
  ID: string;
  LINK: string;
  SUBJECT: string;
}

interface Protocol {
  id: number;
  name: string;
  port: number;
  public: boolean;
}

interface SpecInfo {
  db_name: string;
  pg_ssl_mode: string;
  use_ssl: boolean;
  allow_invalid_cert: boolean;
  autofill: string;
  username_selector: string;
  password_selector: string;
  submit_selector: string;
}

interface SecretInfo {
  ca_cert: string;
  client_cert: string;
  client_key: string;
}

interface Platform {
  id: number;
  name: string;
}

interface Asset {
  id: string;
  address: string;
  name: string;
  org_id: string;
  protocols: Protocol[];
  spec_info: SpecInfo;
  secret_info: SecretInfo;
  platform: Platform;
  domain: string | null;
  comment: string;
  org_name: string;
  is_active: boolean;
}

interface User {
  id: string;
  name: string;
  username: string;
  email: string;
  role: string;
  is_valid: boolean;
  is_active: boolean;
  otp_level: number;
}

interface Interface {
  favicon: string;
  login_image: string;
  login_title: string;
  logo_index: string;
  logo_logout: string;
}

export interface SettingConfig {
  ANNOUNCEMENT?: Announcement;
  ANNOUNCEMENT_ENABLED?: boolean;
  INTERFACE?: Interface;
  SECURITY_SESSION_SHARE?: boolean;
}

export interface DetailMessage {
  asset: Asset;
  setting: SettingConfig;
  user: User;
}

export interface OnlineUser {
  user_id: string;
  user: string;
  created: string;
  remote_addr: string;
  terminal_id: string;
  primary: boolean;
  writable: boolean;
}

export interface ShareUserOptions {
  id: string;

  name: string;

  username: string;
}

export interface ConnectionInfo {
  username: string;
  address: string;
  assetName: string;
  networkSpeed: string;
  cup: string;
  memory: string;
  disk: string;
  io: string;
  uploadSpeed: string;
  downloadSpeed: string;
  sessionId: string;
}

export interface TerminalConfig {
  fontFamily: string;
  fontSize: number | string | null;
  cursorBlink: boolean;
  cursorStyle: 'outline' | 'block' | 'bar' | 'underline' | undefined;
  lineHeight: number;
  themeName: string;
  quickPaste: string;
  backspaceAsCtrlH: string;
  theme: string;
}

export interface ShareInfo {
  shareCode: string;
  enabledShare: boolean;
  onlineUsers: OnlineUser[];
  searchEnabledShareUser: ShareUserOptions[];
}
