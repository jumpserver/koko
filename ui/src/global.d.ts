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
    get_confirmed_session(): ZmodemSession | null;
    static save_to_disk(buffer: Uint8Array[], filename: string): void;
    consume(data: Uint8Array): void;
  }

  export class Browser {
    static send_files(
      session: ZmodemSession,
      files: File,
      opts?: {
        on_offer_response?: (obj: any, xfer: ZmodemTransfer) => void;
        on_file_complete?: (obj: any) => void;
      }
    ): Promise<void>;
  }

  export interface SentryConfig {
    to_terminal?: (octets: string) => void;
    sender?: (octets: Uint8Array) => void;
    on_retract?: () => void;
    on_detect?: (detection: Detection) => void;
  }

  export interface Detection {
    confirm(): ZmodemSession;
  }

  export interface ZmodemSession {
    type: 'send' | 'receive';
    on(event: 'session_end' | 'offer', handler: (arg: any) => void): void;
    start(): void;
    abort(): void;
    close(): void;
  }

  export interface ZmodemTransfer {
    get_details(): { name: string; size: number };
    get_offset(): number;
    accept(): Promise<void>;
    skip(): void;
    on(event: 'input', handler: (payload: Uint8Array) => void): void;
    on(event: 'send_progress', handler: (percent: number) => void): void;
  }
}
