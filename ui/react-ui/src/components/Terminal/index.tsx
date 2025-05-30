import './index.scss';

// @ts-ignore
import xtermTheme from 'xterm-theme';
import useDetail from '@/store/useDetail';

import { useDebounceFn } from 'ahooks';
import { Terminal } from '@xterm/xterm';
import { useCallback, useEffect, useRef, useState } from 'react';
import { getConnectionUrl } from '@/utils';
import { WINDOW_MESSAGE_TYPE } from '@/enums';
import { useSearchParams } from 'react-router';
import { useConnection } from '@/hooks/useConnection';
import { emitterEvent, sendEventToLuna } from '@/utils';

import { FitAddon } from '@xterm/addon-fit';
import { WebglAddon } from '@xterm/addon-webgl';
import { SearchAddon } from '@xterm/addon-search';
import { useFileStatus } from '@/store/useFileStatus';

const TerminalComponent: React.FC = () => {
  const [searchParams] = useSearchParams();

  const { initializeConnection } = useConnection();

  const wsUrl = getConnectionUrl('ws');
  const token = searchParams.get('token');
  const connectionURL = `${wsUrl}/koko/ws/terminal/?token=${token}`;

  const [fileToken, setFileToken] = useState<string>('');

  const lunaId = useRef<string>('');
  const origin = useRef<string>('');
  const fitAddon = useRef<FitAddon>(new FitAddon());
  const webglAddon = useRef<WebglAddon>(new WebglAddon());
  const searchAddon = useRef<SearchAddon>(new SearchAddon());

  const terminal = useRef<Terminal | null>(null);
  const connectionInitialized = useRef<boolean>(false);
  const terminalRef = useRef<HTMLDivElement | null>(null);

  const { terminalConfig } = useDetail();
  const { setToken, setLoaded, resetFileMessage, resetLoadedMessage } = useFileStatus();

  const handleWindowMessage = (message: MessageEvent) => {
    const windowMessage = message.data;

    switch (windowMessage.name) {
      case WINDOW_MESSAGE_TYPE.CMD:
        break;
      case WINDOW_MESSAGE_TYPE.PING:
        lunaId.current = windowMessage.id;
        origin.current = windowMessage.origin;

        setLoaded(false);
        resetFileMessage();
        resetLoadedMessage();
        sendEventToLuna(WINDOW_MESSAGE_TYPE.PONG, '', lunaId.current, origin.current);
        break;
      case WINDOW_MESSAGE_TYPE.FOCUS:
        terminal.current?.focus();
        break;
      case WINDOW_MESSAGE_TYPE.CREATE_FILE_CONNECT_TOKEN:
        console.log('windowMessage.SFTP_Token', windowMessage.SFTP_Token);
        setFileToken(windowMessage.SFTP_Token);
        break;
    }
  };

  const { run: handleResizeDebounced } = useDebounceFn(
    () => {
      if (fitAddon.current) {
        fitAddon.current.fit();
      }
    },
    { wait: 200 }
  );

  const handleGenerateFileToken = useCallback(() => {
    sendEventToLuna(WINDOW_MESSAGE_TYPE.CREATE_FILE_CONNECT_TOKEN, '', lunaId.current, origin.current);
  }, []);

  useEffect(() => {
    const instance = new Terminal({
      allowProposedApi: true,
      rightClickSelectsWord: true,
      scrollback: 5000,
      fontSize: Number(terminalConfig.fontSize),
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
    emitterEvent.on('emit-resize', handleResizeDebounced);
    emitterEvent.on('emit-generate-file-token', handleGenerateFileToken);
    window.addEventListener('resize', handleResizeDebounced);
    window.addEventListener('message', handleWindowMessage);

    if (terminal.current && !connectionInitialized.current) {
      initializeConnection({ wsUrl: connectionURL, terminal: terminal.current });
      connectionInitialized.current = true;
    }

    return () => {
      emitterEvent.off('emit-resize', handleResizeDebounced);
      emitterEvent.off('emit-generate-file-token', handleGenerateFileToken);
      window.removeEventListener('resize', handleResizeDebounced);
      window.removeEventListener('message', handleWindowMessage);
    };
  }, [terminal.current]);

  // 监听配置变化并更新终端设置
  useEffect(() => {
    if (!terminal.current) return;

    const instance = terminal.current;

    instance.options.fontSize = Number(terminalConfig.fontSize);
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

  useEffect(() => {
    if (fileToken) {
      setToken(fileToken);
    }
  }, [fileToken]);

  return <div ref={terminalRef} id="terminal-container" className="h-full"></div>;
};

export default TerminalComponent;
