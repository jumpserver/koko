import type { NavigationGuardNext } from 'vue-router';

import type { ITerminalSettings } from '@/types/modules/terminal.type';
import type { CommandLineConfig, ILocalTerminalConfig } from '@/types/modules/guard.type.ts';

import { useTerminalSettingsStore } from '@/store/modules/terminalSettings.ts';

/**
 * @description 获取本地 Terminal 配置
 */
function getLocalKokoSetting() {
  const terminalSettingsStore = useTerminalSettingsStore();
  const localTerminalSetting = localStorage.getItem('LunaSetting');

  const { setDefaultTerminalConfig } = terminalSettingsStore;

  if (localTerminalSetting) {
    const parsedSetting: ILocalTerminalConfig = JSON.parse(localTerminalSetting);
    const commandLine: CommandLineConfig = parsedSetting.command_line;

    let fontSize = 13;

    if (commandLine) {
      fontSize = commandLine.character_terminal_font_size;
      setDefaultTerminalConfig('quickPaste', commandLine.is_right_click_quickly_paste ? '1' : '0');
      setDefaultTerminalConfig('backspaceAsCtrlH', commandLine.is_backspace_as_ctrl_h ? '1' : '0');
    }

    if (!fontSize || fontSize < 5 || fontSize > 50) {
      fontSize = 13;
    }

    setDefaultTerminalConfig('fontSize', fontSize);
  }
}

export function guard(next: NavigationGuardNext) {
  try {
    getLocalKokoSetting();
    next();
  } catch (error) {
    throw new Error(`Initialization failed: ${error}`);
  }
}

export function getLocalDefaultKokoSetting(): CommandLineConfig {
  const localTerminalSetting = localStorage.getItem('LunaSetting');
  const defaultCommandLine: CommandLineConfig = {
    character_terminal_font_size: 13,
    is_backspace_as_ctrl_h: false,
    is_right_click_quickly_paste: true,
    terminal_theme_name: 'Default',
  };

  if (localTerminalSetting) {
    const parsedSetting: ILocalTerminalConfig = JSON.parse(localTerminalSetting);
    const commandLine: CommandLineConfig = parsedSetting.command_line;

    let fontSize = 13;

    if (commandLine) {
      const fontSize = commandLine.character_terminal_font_size;
      const is_backspace_as_ctrl_h = commandLine.is_backspace_as_ctrl_h;
      const is_right_click_quickly_paste = commandLine.is_right_click_quickly_paste;
      const terminal_theme_name = commandLine.terminal_theme_name;

      defaultCommandLine.character_terminal_font_size = fontSize || 13;
      defaultCommandLine.is_backspace_as_ctrl_h = is_backspace_as_ctrl_h || false;
      defaultCommandLine.terminal_theme_name = terminal_theme_name || 'Default';
      defaultCommandLine.is_right_click_quickly_paste = is_right_click_quickly_paste
        ? is_right_click_quickly_paste
        : false;
    }

    if (!fontSize || fontSize < 5 || fontSize > 50) {
      fontSize = 13;
    }
  }

  return defaultCommandLine;
}

export function getDefaultTerminalConfig(): ITerminalSettings {
  const defaultCommandLine = getLocalDefaultKokoSetting();

  return {
    fontSize: defaultCommandLine.character_terminal_font_size,
    lineHeight: 1.2,
    fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
    themeName: defaultCommandLine.terminal_theme_name,
    quickPaste: defaultCommandLine.is_right_click_quickly_paste ? '1' : '0',
    ctrlCAsCtrlZ: '0',
    backspaceAsCtrlH: defaultCommandLine.is_backspace_as_ctrl_h ? '1' : '0',
    theme: defaultCommandLine.terminal_theme_name,
  };
}
