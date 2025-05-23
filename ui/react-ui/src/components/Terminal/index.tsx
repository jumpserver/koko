import './index.scss';

// @ts-ignore
import xtermTheme from 'xterm-theme';
import useDetail from '@/store/useDetail';

import { Terminal } from '@xterm/xterm';
import { useEffect, useRef } from 'react';
import { getConnectionUrl } from '@/utils';
import { useSearchParams } from 'react-router';
import { useConnection } from '@/hooks/useConnection';

import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';
import { SearchAddon } from '@xterm/addon-search';

const TerminalComponent: React.FC = () => {
  const [searchParams] = useSearchParams();

  const { initializeConnection } = useConnection();

  const wsUrl = getConnectionUrl('ws');
  const token = searchParams.get('token');
  const connectionURL = `${wsUrl}/koko/ws/terminal/?token=${token}`;

  const fitAddon = useRef<FitAddon>(new FitAddon());
  const webglAddon = useRef<WebglAddon>(new WebglAddon());
  const searchAddon = useRef<SearchAddon>(new SearchAddon());

  const terminal = useRef<Terminal | null>(null);
  const connectionInitialized = useRef<boolean>(false);
  const terminalRef = useRef<HTMLDivElement | null>(null);

  const { terminalConfig } = useDetail();

  const handleWindowMessage = () => {};

  useEffect(() => {
    const instance = new Terminal({
      allowProposedApi: true,
      rightClickSelectsWord: true,
      scrollback: 5000,
      fontSize: terminalConfig.fontSize,
      fontFamily: terminalConfig.fontFamily,
      lineHeight: terminalConfig.lineHeight,
      cursorBlink: terminalConfig.cursorBlink,
      theme: terminalConfig.theme ? xtermTheme[terminalConfig.theme] : undefined,
      cursorStyle: terminalConfig.cursorStyle === 'outline' ? 'block' : terminalConfig.cursorStyle
    });

    instance.loadAddon(fitAddon.current);
    instance.loadAddon(webglAddon.current);
    instance.loadAddon(searchAddon.current);

    if (terminalRef.current) {
      instance.open(terminalRef.current);
      fitAddon.current.fit();
      terminal.current = instance;
    }

    return () => {
      instance.dispose();
    };
  }, []);

  // 初始化连接
  useEffect(() => {
    window.addEventListener('message', handleWindowMessage);

    if (terminal.current && !connectionInitialized.current) {
      initializeConnection({ wsUrl: connectionURL, terminal: terminal.current });
      connectionInitialized.current = true;
    }

    return () => {
      window.removeEventListener('message', handleWindowMessage);
    };
  }, [terminal.current]);

  // 监听配置变化并更新终端设置
  useEffect(() => {
    if (!terminal.current) return;

    const instance = terminal.current;

    instance.options.fontSize = terminalConfig.fontSize;
    instance.options.fontFamily = terminalConfig.fontFamily;
    instance.options.lineHeight = terminalConfig.lineHeight;
    instance.options.cursorBlink = terminalConfig.cursorBlink;

    if (terminalConfig.theme) {
      instance.options.theme = xtermTheme[terminalConfig.theme];
    }

    if (terminalConfig.cursorStyle) {
      instance.options.cursorStyle = terminalConfig.cursorStyle === 'outline' ? 'block' : terminalConfig.cursorStyle;
    }

    setTimeout(() => {
      if (fitAddon.current) {
        try {
          fitAddon.current.fit();
        } catch (e) {
          console.error('Failed to fit terminal:', e);
        }
      }
    }, 0);
  }, [terminalConfig]);

  return <div ref={terminalRef} id="terminal-container" className="h-full"></div>;
};

export default TerminalComponent;
