import { Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';
import { wsIsActivated } from '@/components/Terminal/helper';

import ZmodemBrowser, {
  Detection,
  Sentry,
  SentryConfig,
  ZmodemSession
} from 'nora-zmodemjs/src/zmodem_browser';

const { debug, info } = useLogger('useSentry');

export const useSentry = (
  lastSendTime: Ref<Date>
): {
  generateSentry: (ws: WebSocket, terminal: Terminal) => SentryConfig;
  createSentry: (ws: WebSocket, terminal: Terminal) => Sentry;
} => {
  const generateSentry = (ws: WebSocket, terminal: Terminal): SentryConfig => {
    return {
      to_terminal: (octets: string) => {
        terminal.write(octets);
        // if (sentryRef && !sentryRef.get_confirmed_session()) return ;
      },
      sender: (octets: Uint8Array) => {
        if (!wsIsActivated(ws)) return debug('WebSocket Closed');
        lastSendTime.value = new Date();
        debug(`octets: ${octets}`);
        ws.send(new Uint8Array(octets));
      },
      on_retract: () => info('Zmodem Retract'),
      on_detect: (detection: Detection) => {
        const zsession: ZmodemSession = detection.confirm();
        terminal.write('\r\n');
        if (zsession.type === 'send') {
          // handleSendSession(zsession);
        } else {
          // handleReceiveSession(zsession);
        }
      }
    };
  };

  const createSentry = (ws: WebSocket, terminal: Terminal): Sentry => {
    const sentryConfig: SentryConfig = generateSentry(ws, terminal);
    return new ZmodemBrowser.Sentry(sentryConfig);
  };

  return {
    createSentry,
    generateSentry
  };
};
