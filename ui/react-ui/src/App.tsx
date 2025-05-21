import { useEffect } from 'react';
import { RouterProvider } from 'react-router';
import { App as AntApp, ConfigProvider } from 'antd';

import router from './routes';
import useTerminalSetting from '@/store/useTerminalSetting';

import type { LocalTerminalConfig, CommandLineConfig } from '@/types/terminal.type';

export const App = () => {
  const { setDefaultTerminalConfig } = useTerminalSetting();

  useEffect(() => {
    const localTerminalSetting = localStorage.getItem('LunaSetting');

    if (localTerminalSetting) {
      const parsedSetting: LocalTerminalConfig = JSON.parse(localTerminalSetting);
      const commandLine: CommandLineConfig = parsedSetting.command_line;

      let fontSize = 0;

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
  }, []);

  return (
    <ConfigProvider>
      <AntApp message={{ maxCount: 1 }} notification={{ maxCount: 1 }}>
        <RouterProvider router={router} />
      </AntApp>
    </ConfigProvider>
  );
};
