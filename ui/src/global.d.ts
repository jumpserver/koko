interface Window {
  Reconnect: () => void;

  SendTerminalData: (data: any) => void;
}

declare module 'xterm-theme' {
  const themes: { [key: string]: any };
  export default themes;
}

declare module 'nora-zmodemjs/src/zmodem_browser' {
  export class Sentry {
    constructor(config: SentryConfig);
    get_confirmed_session(): any;
    consume(data: any): void;
  }

  export interface SentryConfig {
    to_terminal: (octets: string) => void;
    sender: (octets: Uint8Array) => void;
    on_retract: () => void;
    on_detect: (detection: any) => void;
  }

  export interface Detection {
    confirm(): ZmodemSession;
  }

  export interface ZmodemSession {
    type: 'send' | 'receive';
  }
}
