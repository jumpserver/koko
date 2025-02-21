import { TreeOption } from 'naive-ui';

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
interface Announcement {
  CONTENT: string;
  ID: string;
  LINK: string;
  SUBJECT: string;
}
interface InterfaceSettings {
  login_title: string;
  logo_logout: string;
  logo_index: string;
  login_image: string;
  favicon: string;
}
interface SettingAnnouncement {
  ID: string;
  SUBJECT: string;
  CONTENT: string;
  LINK: string;
  DATE_START: string;
  DATE_END: string;
}
interface Setting {
  INTERFACE: InterfaceSettings;
  SECURITY_WATERMARK_ENABLED: boolean;
  SECURITY_SESSION_SHARE: boolean;
  ANNOUNCEMENT_ENABLED: boolean;
  ANNOUNCEMENT: SettingAnnouncement;
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

export interface IFileManage {
  id: string;
  type: string;
  // data: IFileManageConnectData | IFileManageSftpFileItem;
  data: string;
  raw?: any;
  err: string;
  prompt: string;
  interrupt: boolean;
  k8s_id: string;
  namespace: string;
  pod: string;
  container: string;
  cmd: string;
  current_path: string;
}
export interface IFileManageConnectData {
  user: User;
  setting: Setting;
  asset: Asset;
}

export interface IFileManageSftpFileItem {
  name: string;
  size: string;
  perm: string;
  mod_time: string;
  type: string;
  is_dir: boolean;
}
