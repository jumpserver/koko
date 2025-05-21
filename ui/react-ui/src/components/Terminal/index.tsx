import './index.scss';

import { Terminal } from '@xterm/xterm';
import { useEffect, useRef } from 'react';
import { getConnectionUrl } from '@/utils';
import { useSearchParams } from 'react-router';
import { useTerminal } from '@/hooks/useTerminal';
import { useConnection } from '@/hooks/useConnection';
import { TERMINAL_MESSAGE_TYPE } from '@/enums';

const TerminalComponent: React.FC = () => {
  const [searchParams] = useSearchParams();
  const { createTerminal, fitAddon } = useTerminal();
  const { initializeConnection, socketRef, terminalId } = useConnection();

  const handleWindowMessage = () => {};

  useEffect(() => {
    window.addEventListener('message', handleWindowMessage);

    const el = document.getElementById('terminal-container');

    if (!el) {
      return;
    }

    const terminalInstance = createTerminal(el);

    const wsUrl = getConnectionUrl('ws');
    const token = searchParams.get('token');

    if (!terminalInstance) {
      return;
    }

    initializeConnection({ wsUrl: `${wsUrl}/koko/ws/terminal/?token=${token}`, terminal: terminalInstance });

    // terminalInstance.onResize(({ cols, rows }) => {
    //   fitAddon.fit();

    //   const resizeData = { cols, rows };

    //   if (socketRef.current) {
    //     socketRef.current.send(
    //       JSON.stringify({
    //         id: terminalId,
    //         type: TERMINAL_MESSAGE_TYPE.TERMINAL_RESIZE,
    //         data: JSON.stringify(resizeData)
    //       })
    //     );
    //   }
    // });

    return () => {
      if (terminalInstance) {
        terminalInstance.dispose();
      }
    };
  }, []);

  return <div id="terminal-container" className="w-screen h-screen"></div>;
};

export default TerminalComponent;
