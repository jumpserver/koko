import { useLayoutEffect, useRef } from 'react';
import { Terminal } from '@xterm/xterm';

export const MainContainer: React.FC = () => {
  const terminalRef = useRef<Terminal | null>(null);

  useLayoutEffect(() => {
    if (!terminalRef.current) {
      terminalRef.current = new Terminal();
      terminalRef.current.open(document.getElementById('terminal') as HTMLElement);
      terminalRef.current.write('Hello from \x1B[1;3;31mxterm.js\x1B[0m $ ');
    }

    return () => {
      if (terminalRef.current) {
        terminalRef.current.dispose();
        terminalRef.current = null;
      }
    };
  }, []);

  return <div id="terminal" className="w-full h-full"></div>;
};
