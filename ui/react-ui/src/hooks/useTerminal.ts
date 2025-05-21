// @ts-ignore
import xtermTheme from 'xterm-theme';
import { Terminal } from '@xterm/xterm';
import { useState, useEffect } from 'react';
import { FitAddon } from '@xterm/addon-fit';

export const useTerminal = () => {
  const [el, setEl] = useState<HTMLElement | null>(null);
  const [terminal, setTerminal] = useState<Terminal | null>(null);
  const [fitAddon] = useState(() => new FitAddon());

  const createTerminal = (el: HTMLElement, config?: any): Terminal => {
    const instance = new Terminal({
      allowProposedApi: true,
      rightClickSelectsWord: true,
      scrollback: 5000,
      fontSize: 14,
      lineHeight: 1,
      fontFamily: 'monospace',
      theme: {
        foreground: '#c0caf5', // 浅色前景
        background: '#1a1b26', // 背景色
        cursor: '#c0caf5', // 光标颜色
        cursorAccent: '#f7768e', // 光标强调色（比如块状光标）
        selectionBackground: '#4b5263', // 选中背景
        selectionForeground: '#ffffff', // 选中前景

        black: '#1e222a',
        red: '#f7768e',
        green: '#73daca',
        yellow: '#e0af68',
        blue: '#7aa2f7',
        magenta: '#bb9af7',
        cyan: '#7dcfff',
        white: '#a9b1d6',

        brightBlack: '#414868',
        brightRed: '#f7768e',
        brightGreen: '#73daca',
        brightYellow: '#e0af68',
        brightBlue: '#7aa2f7',
        brightMagenta: '#bb9af7',
        brightCyan: '#7dcfff',
        brightWhite: '#c0caf5',

        extendedAnsi: [
          // 可根据需要添加扩展色，下面是示例
          '#001f3f',
          '#0074D9',
          '#7FDBFF',
          '#39CCCC',
          '#3D9970',
          '#2ECC40',
          '#FFDC00',
          '#FF851B',
          '#FF4136',
          '#85144b',
          '#F012BE',
          '#B10DC9',
          '#AAAAAA',
          '#DDDDDD',
          '#FFFFFF'
        ]
      },
      ...config
    });

    instance.loadAddon(fitAddon);

    instance.open(el);
    fitAddon.fit();

    setEl(el);
    setTerminal(instance);
    return instance;
  };

  const setTerminalTheme = (themeName: string) => {
    if (!terminal) return;

    const theme = xtermTheme[themeName];

    terminal.options.theme = theme;
  };

  useEffect(() => {
    if (!el) return;

    el.addEventListener('mouseenter', (_e: MouseEvent) => {
      console.log('mouseenter');
      fitAddon.fit();
      terminal?.focus();
    });

    el.addEventListener('contextmenu', (e: MouseEvent) => {
      e.preventDefault();
    });
  }, [el]);

  return {
    terminal,
    fitAddon,
    createTerminal,
    setTerminalTheme
  };
};
