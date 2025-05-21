import './index.scss';

import { Terminal } from '@xterm/xterm';
import { useEffect, useRef } from 'react';
import { getConnectionUrl } from '@/utils';
import { useSearchParams } from 'react-router';
import { useConnection } from '@/hooks/useConnection';

import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';
import { SearchAddon } from '@xterm/addon-search';

import useTerminalSetting from '@/store/useTerminalSetting';

const TerminalComponent: React.FC = () => {
  const [searchParams] = useSearchParams();

  const { initializeConnection } = useConnection();

  const wsUrl = getConnectionUrl('ws');
  const token = searchParams.get('token');
  const connectionURL = `${wsUrl}/koko/ws/terminal/?token=${token}`;

  const terminal = useRef<Terminal | null>(null);
  const terminalRef = useRef<HTMLDivElement | null>(null);
  const fitAddon = useRef<FitAddon>(new FitAddon());
  const webglAddon = useRef<WebglAddon>(new WebglAddon());
  const searchAddon = useRef<SearchAddon>(new SearchAddon());

  const { fontSize, lineHeight, cursorInactiveStyle, fontFamily } = useTerminalSetting();

  const handleWindowMessage = () => {};

  useEffect(() => {
    const instance = new Terminal({
      allowProposedApi: true,
      rightClickSelectsWord: true,
      scrollback: 5000,
      fontSize,
      lineHeight,
      cursorInactiveStyle,
      fontFamily,
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
      }
    });

    instance.loadAddon(fitAddon.current);
    instance.loadAddon(webglAddon.current);
    instance.loadAddon(searchAddon.current);

    if (!terminalRef.current) {
      return;
    }

    instance.open(terminalRef.current);
    fitAddon.current.fit();

    terminal.current = instance;

    return () => {
      instance.dispose();
    };
  }, []);

  useEffect(() => {
    window.addEventListener('message', handleWindowMessage);

    if (!terminal.current) {
      return;
    }

    initializeConnection({ wsUrl: connectionURL, terminal: terminal.current });

    return () => {
      window.removeEventListener('message', handleWindowMessage);
    };
  }, []);

  return <div ref={terminalRef} id="terminal-container" className="h-screen"></div>;
};

export default TerminalComponent;
