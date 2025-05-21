// @ts-ignore
import xtermTheme from 'xterm-theme';
import { emitterEvent } from '@/utils';
import { Terminal } from '@xterm/xterm';
import { useState, useEffect } from 'react';
import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';
import { SearchAddon } from '@xterm/addon-search';

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
        background: '#121414',
        foreground: '#ffffff',
        black: '#2e3436',
        red: '#cc0000',
        green: '#4e9a06',
        yellow: '#c4a000',
        blue: '#3465a4',
        magenta: '#75507b',
        cyan: '#06989a',
        white: '#d3d7cf',
        brightBlack: '#555753',
        brightRed: '#ef2929',
        brightGreen: '#8ae234',
        brightYellow: '#fce94f',
        brightBlue: '#729fcf',
        brightMagenta: '#ad7fa8',
        brightCyan: '#34e2e2',
        brightWhite: '#eeeeec'
      },
      ...config
    });

    instance.loadAddon(fitAddon);
    instance.loadAddon(new WebglAddon());
    instance.loadAddon(new SearchAddon());

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
