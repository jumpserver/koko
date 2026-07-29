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
    get_confirmed_session: () => ZmodemSession | null;
    consume: (data: number[] | ArrayBuffer) => void;
  }

  export class Browser {
    static send_files: (
      session: ZmodemSession,
      files: File[],
      opts?: {
        on_offer_response?: (obj: any, xfer: Transfer) => void;
        on_file_complete?: (obj: any) => void;
      }
    ) => Promise<void>;

    static save_to_disk: (buffer: Uint8Array[], filename: string) => void;
  }

  export interface Detection {
    confirm: () => ZmodemSession;
  }

  export interface Transfer {
    get_details: () => { name: string; size: number };
    get_offset: () => number;
    accept: () => Promise<void>;
    skip: () => void;
    send: (payload: Uint8Array) => void;
    end: (payload?: Uint8Array) => Promise<void>;
    on: ((event: 'input', handler: (payload: Uint8Array) => void) => void) &
      ((event: 'send_progress', handler: (percent: number) => void) => void);
  }

  export interface ZmodemSession {
    type: 'send' | 'receive';
    on: (event: 'session_end' | 'offer', handler: (arg: any) => void) => void;
    start: () => void;
    abort: () => void;
    aborted: () => boolean;
    close: () => Promise<void>;
    send_offer: (details: {
      name: string;
      size: number;
      mtime: Date;
      files_remaining: number;
      bytes_remaining: number;
    }) => Promise<Transfer | undefined>;
  }

  export interface SentryConfig {
    to_terminal?: (octets: number[] | Uint8Array) => void;
    sender?: (octets: number[] | Uint8Array) => void;
    on_retract?: () => void;
    on_detect?: (detection: Detection) => void;
  }
}
