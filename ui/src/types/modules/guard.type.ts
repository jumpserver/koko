interface BasicConfig {
  is_async_asset_tree: boolean;
  connect_default_open_method: string;
}

interface GraphicsConfig {
  rdp_resolution: string;
  keyboard_layout: string;
  rdp_client_option: string[];
  rdp_color_quality: string;
  rdp_smart_size: number;
  applet_connection_method: string;
  file_name_conflict_resolution: string;
}

export interface CommandLineConfig {
  character_terminal_font_size: number;
  is_backspace_as_ctrl_h: boolean;
  is_right_click_quickly_paste: boolean;
  terminal_theme_name: string;
}

export interface ILocalTerminalConfig {
  commandExecution: boolean;
  isSkipAllManualPassword: string;
  sqlClient: string;
  basic: BasicConfig;
  graphics: GraphicsConfig;
  command_line: CommandLineConfig;
}
