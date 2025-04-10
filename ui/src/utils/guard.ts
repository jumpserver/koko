import { NavigationGuardNext } from 'vue-router';
import i18n from '@/languages/index';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings.ts';

import type { ILocalTermialConfig, CommandLineConfig } from '@/types/modules/guard.type.ts';

/**
 * @description 获取本地 Termianl 配置
 */
const getLocalKokoSetting = () => {
  const terminalSettingsStore = useTerminalSettingsStore();
  const localTerminalSetting = localStorage.getItem('LunaSetting');

  const { setDefaultTerminalConfig } = terminalSettingsStore;

  if (localTerminalSetting) {
    const parsedSetting: ILocalTermialConfig = JSON.parse(localTerminalSetting);
    const commandLine: CommandLineConfig = parsedSetting.command_line;

    let fontSize = 0

    if (commandLine) {
      fontSize = commandLine.character_terminal_font_size;
      setDefaultTerminalConfig('quickPaste', commandLine.is_right_click_quickly_paste ? '1' : '0')
      setDefaultTerminalConfig('backspaceAsCtrlH', commandLine.is_backspace_as_ctrl_h ? '1' : '0')
    }

    if (!fontSize || fontSize < 5 || fontSize > 50) {
      fontSize = 13;
    }

    setDefaultTerminalConfig('fontSize', fontSize);
  }
}

export const guard = (next: NavigationGuardNext) => {
  try {
    getLocalKokoSetting();
    next();
  } catch (error) {
    throw new Error(`Initialization failed: ${error}`);
  }
};
