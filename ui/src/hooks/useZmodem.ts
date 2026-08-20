import type { Ref } from 'vue';
import type { Terminal } from '@xterm/xterm';
import type { ConfigProviderProps, UploadFileInfo } from 'naive-ui';
import type { Detection, Transfer, ZmodemSession } from 'nora-zmodemjs/src/zmodem_browser';

import { useI18n } from 'vue-i18n';
import { computed, h, ref } from 'vue';
import prettyBytes from 'pretty-bytes';
import { createDiscreteApi, darkTheme } from 'naive-ui';
import ZmodemBrowser from 'nora-zmodemjs/src/zmodem_browser';

import { formatMessage } from '@/utils';
import { AsciiCtrlC, MAX_TRANSFER_SIZE } from '@/utils/config';
import ZmodemUpload from '@/components/ZmodemUpload/index.vue';
import { FORMATTER_MESSAGE_TYPE } from '@/types/modules/message.type';

const UPLOAD_CHUNK_SIZE = 64 * 1024;
const SOCKET_BUFFER_HIGH_WATERMARK = 256 * 1024;
const SOCKET_BUFFER_LOW_WATERMARK = 64 * 1024;
const SOCKET_DRAIN_TIMEOUT = 30 * 1000;
const PEER_RESPONSE_TIMEOUT = 30 * 1000;
const DRAIN_FALLBACK_TIMEOUT = 10 * 1000;

const formatTransferSize = (size: number) => prettyBytes(size, { binary: true });

const createAbortError = () => new DOMException('File transfer cancelled', 'AbortError');

const isAbortError = (error: unknown) => error instanceof DOMException && error.name === 'AbortError';

type ZmodemSendSession = ZmodemSession & {
  _current_transfer: Transfer;
  _file_offset: number;
};

interface ZmodemHeader {
  _bytes4?: number[];
}

interface ZmodemHeaderFactory {
  build: (name: string, ...args: unknown[]) => ZmodemHeader;
  kokoClobberPatched?: boolean;
}

/**
 * nora-zmodemjs 没有暴露 ZFILE 的管理选项，默认值会让部分 lrzsz 在同名文件上一直等待。
 * 为上传的 ZFILE 设置 ZMCLOB，与命令行 rz 覆盖文件的行为保持一致。
 */
const enableUploadOverwrite = () => {
  const headerFactory = (ZmodemBrowser as unknown as { Header?: ZmodemHeaderFactory }).Header;
  if (!headerFactory || headerFactory.kokoClobberPatched) {
    return;
  }

  const originalBuild = headerFactory.build.bind(headerFactory);
  headerFactory.build = (name: string, ...args: unknown[]) => {
    const header = originalBuild(name, ...args);
    if (name === 'ZFILE' && header._bytes4?.length === 4) {
      header._bytes4[2] = 0x04;
    }
    return header;
  };
  headerFactory.kokoClobberPatched = true;
};

export const useZmodem = () => {
  enableUploadOverwrite();

  const { t } = useI18n();

  const fileInfo = ref<File | null>(null);
  const sentryRef = ref<ZmodemBrowser.Sentry | null>(null);
  const activeSession = ref<ZmodemSession | null>(null);
  const activeTransferSession = ref<ZmodemSession | null>(null);
  const activeTransferController = ref<AbortController | null>(null);
  const draining = ref(false);

  let lastPercent = -1;
  let drainTimer: ReturnType<typeof setTimeout> | null = null;

  const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
    theme: darkTheme,
  }));

  const { message, modal } = createDiscreteApi(['message', 'modal'], {
    configProviderProps: configProviderPropsRef,
  });

  const resetProgress = () => {
    lastPercent = -1;
  };

  const stopDraining = () => {
    draining.value = false;
    if (drainTimer) {
      clearTimeout(drainTimer);
      drainTimer = null;
    }
  };

  const startDraining = () => {
    draining.value = true;
    if (drainTimer) {
      clearTimeout(drainTimer);
    }
    drainTimer = setTimeout(stopDraining, DRAIN_FALLBACK_TIMEOUT);
  };

  const finishDraining = () => {
    if (!draining.value) {
      return;
    }
    if (drainTimer) {
      clearTimeout(drainTimer);
    }
    drainTimer = setTimeout(stopDraining, DRAIN_FALLBACK_TIMEOUT);
  };

  const isReadableTerminalText = (octets: number[] | Uint8Array) => {
    const bytes = octets instanceof Uint8Array ? octets : new Uint8Array(octets);
    // ZMODEM 的十六进制帧以 ** ZDLE B 开头，即使是 ASCII 也不能作为 shell 输出显示。
    if (bytes.length >= 4 && bytes[0] === 0x2A && bytes[1] === 0x2A && bytes[2] === 0x18 && bytes[3] === 0x42) {
      return false;
    }

    try {
      const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
      const visibleLength = Array.from(text).filter(char => char >= ' ' && char !== '\x7F').length;
      return (text.includes('\r') || text.includes('\n')) && visibleLength >= 4 && visibleLength / text.length >= 0.6;
    }
    catch {
      return false;
    }
  };

  /**
   * 只释放浏览器端状态，不向对端发送协议消息。
   */
  const cleanupSession = (session?: ZmodemSession) => {
    let cleaned = false;
    if (!session || activeSession.value === session) {
      activeSession.value = null;
      cleaned = true;
    }
    if (!session || activeTransferSession.value === session) {
      activeTransferSession.value = null;
      activeTransferController.value = null;
      cleaned = true;
    }
    if (cleaned) {
      resetProgress();
    }
  };

  const abortSession = (session: ZmodemSession | null = activeSession.value) => {
    if (
      session
      && activeSession.value !== session
      && activeTransferSession.value !== session
    ) {
      return;
    }

    startDraining();
    if (!session || activeTransferSession.value === session) {
      activeTransferController.value?.abort();
    }

    if (session) {
      try {
        if (!session.aborted()) {
          session.abort();
        }
      }
      catch (error) {
        console.warn('Error aborting ZMODEM session:', error);
      }
    }

    cleanupSession(session ?? undefined);
  };

  const cancelSendSession = (session: ZmodemSession, socket: WebSocket) => {
    // 先让 Koko 进入残余输入丢弃状态，再用 CAN 标记浏览器发送队列的末尾。
    if (socket.readyState === WebSocket.OPEN) {
      socket.send(
        formatMessage('', FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, String.fromCharCode(AsciiCtrlC)),
      );
    }
    abortSession(session);
  };

  const handleSessionEnd = (session: ZmodemSession, terminal: Terminal) => {
    // 上传完成由 handleUpload 输出 100% 并换行，避免留下 99% 和 100% 两条进度。
    if (activeTransferSession.value !== session) {
      terminal.write('\r\n');
    }
    // close() 会先同步触发 session_end，再 resolve Promise；延后一轮以免把正常完成误判为取消。
    setTimeout(() => {
      if (activeTransferSession.value === session) {
        activeTransferController.value?.abort();
      }
      cleanupSession(session);
    }, 0);
  };

  const abortActiveSession = () => {
    abortSession();
  };

  const isActiveSession = () => Boolean(activeSession.value || activeTransferSession.value);

  const abortableDelay = (timeout: number, signal: AbortSignal) =>
    new Promise<void>((resolve, reject) => {
      if (signal.aborted) {
        reject(createAbortError());
        return;
      }

      let timer: ReturnType<typeof setTimeout>;
      const onAbort = () => {
        clearTimeout(timer);
        reject(createAbortError());
      };
      timer = setTimeout(() => {
        signal.removeEventListener('abort', onAbort);
        resolve();
      }, timeout);

      signal.addEventListener('abort', onAbort, { once: true });
    });

  const waitForSocketDrain = async (socket: WebSocket, signal: AbortSignal) => {
    if (socket.readyState !== WebSocket.OPEN) {
      throw new Error(t('WebSocket connection is closed, please refresh the page'));
    }

    if (socket.bufferedAmount <= SOCKET_BUFFER_HIGH_WATERMARK) {
      // 每个文件块都让出一次事件循环，避免连续编码大文件阻塞浏览器主线程。
      await abortableDelay(0, signal);
      return;
    }

    const startedAt = Date.now();
    while (socket.bufferedAmount > SOCKET_BUFFER_LOW_WATERMARK) {
      if (socket.readyState !== WebSocket.OPEN) {
        throw new Error(t('WebSocket connection is closed, please refresh the page'));
      }
      if (Date.now() - startedAt >= SOCKET_DRAIN_TIMEOUT) {
        throw new Error('ZMODEM WebSocket drain timeout');
      }
      await abortableDelay(20, signal);
    }
  };

  const waitForPeer = <T>(promise: Promise<T>, signal: AbortSignal) =>
    new Promise<T>((resolve, reject) => {
      if (signal.aborted) {
        reject(createAbortError());
        return;
      }

      let timer: ReturnType<typeof setTimeout>;
      const onAbort = () => {
        clearTimeout(timer);
        reject(createAbortError());
      };
      timer = setTimeout(() => {
        signal.removeEventListener('abort', onAbort);
        reject(new Error('ZMODEM peer response timeout'));
      }, PEER_RESPONSE_TIMEOUT);

      signal.addEventListener('abort', onAbort, { once: true });
      promise.then(
        (value) => {
          clearTimeout(timer);
          signal.removeEventListener('abort', onAbort);
          resolve(value);
        },
        (error) => {
          clearTimeout(timer);
          signal.removeEventListener('abort', onAbort);
          reject(error);
        },
      );
    });

  const writeUploadProgress = (file: File, sent: number, terminal: Terminal, completed = false) => {
    const percent = completed ? 100 : file.size === 0 ? 0 : Math.min(99, Math.floor((sent / file.size) * 100));

    if (percent === lastPercent) {
      return;
    }

    const completedLength = Math.floor(percent / 2);
    const progressBar = `${'='.repeat(completedLength)}${' '.repeat(50 - completedLength)}`;
    const content = `${t('Upload')} ${file.name}: ${formatTransferSize(file.size)} ${percent}% [${progressBar}]`;

    terminal.write(`\r${content}`);
    lastPercent = percent;
  };

  const terminalProgress = (transfer: Transfer, terminal: Terminal, previousPercent: number) => {
    const detail = transfer.get_details();
    const offset = transfer.get_offset();
    const percent = detail.size === 0 || detail.size === offset ? 100 : Math.floor((offset / detail.size) * 100);

    if (percent === previousPercent) {
      return previousPercent;
    }

    const content = `${t('Download')} ${detail.name}: ${formatTransferSize(detail.size)} ${percent}% `;
    terminal.write(`\r${content}`);

    return percent;
  };

  /**
   * 分块读取文件，并根据 WebSocket 发送队列做背压。
   */
  const handleUpload = async (session: ZmodemSession, terminal: Terminal, socket: WebSocket, signal: AbortSignal) => {
    const file = fileInfo.value;
    if (!file) {
      throw new Error(t('MustSelectOneFile'));
    }
    if (file.size >= MAX_TRANSFER_SIZE) {
      throw new Error(`${t('ExceedTransferSize')}: ${formatTransferSize(MAX_TRANSFER_SIZE)}`);
    }

    resetProgress();
    const transfer = await waitForPeer(
      session.send_offer({
        name: file.name,
        size: file.size,
        mtime: new Date(file.lastModified),
        files_remaining: 1,
        bytes_remaining: file.size,
      }),
      signal,
    );

    if (!transfer) {
      await waitForPeer(session.close(), signal);
      terminal.write(`\r\n${t('ZmodemUploadSkipped')}\r\n`);
      message.warning(t('ZmodemUploadSkipped'));
      cleanupSession(session);
      return;
    }

    let offset = transfer.get_offset();
    if (!Number.isSafeInteger(offset) || offset < 0 || offset > file.size) {
      throw new Error(`Invalid ZMODEM resume offset: ${offset}`);
    }
    if (offset > 0) {
      // nora-zmodemjs 暴露了 Transfer 偏移，但没有同步其发送 Session 的内部偏移。
      (session as ZmodemSendSession)._file_offset = offset;
      writeUploadProgress(file, offset, terminal);
    }

    if (offset === file.size) {
      (session as ZmodemSendSession)._current_transfer = transfer;
      const peerResponse = transfer.end(new Uint8Array());
      await waitForSocketDrain(socket, signal);
      await waitForPeer(peerResponse, signal);
    }
    else {
      while (offset < file.size) {
        if (signal.aborted) {
          throw createAbortError();
        }

        await waitForSocketDrain(socket, signal);

        const nextOffset = Math.min(offset + UPLOAD_CHUNK_SIZE, file.size);
        const payload = new Uint8Array(await file.slice(offset, nextOffset).arrayBuffer());

        if (signal.aborted) {
          throw createAbortError();
        }

        if (nextOffset === file.size) {
          (session as ZmodemSendSession)._current_transfer = transfer;
          const peerResponse = transfer.end(payload);
          offset = nextOffset;
          writeUploadProgress(file, offset, terminal);
          await waitForSocketDrain(socket, signal);
          await waitForPeer(peerResponse, signal);
        }
        else {
          // 当前依赖分支会在每次 send() 后清空该指针，分块续传前必须重新关联。
          (session as ZmodemSendSession)._current_transfer = transfer;
          transfer.send(payload);
          offset = nextOffset;
          writeUploadProgress(file, offset, terminal);
        }
      }
    }

    writeUploadProgress(file, file.size, terminal, true);
    await waitForPeer(session.close(), signal);
    terminal.write('\r\n');
    message.success(`${t('EndFileTransfer')}: ${t('UploadSuccess')} ${file.name}`, {
      duration: 2000,
    });
    cleanupSession(session);
  };

  const handleUploadError = (error: unknown, session: ZmodemSession) => {
    const isCurrent = activeSession.value === session || activeTransferSession.value === session;
    abortSession(session);
    if (isCurrent && !isAbortError(error)) {
      const content = error instanceof Error ? error.message : String(error);
      message.error(content || t('File transfer error, file transfer interrupted'));
    }
  };

  const startUpload = (session: ZmodemSession, terminal: Terminal, socket: WebSocket, onFinished: () => void) => {
    const controller = new AbortController();
    activeTransferSession.value = session;
    activeTransferController.value = controller;

    void handleUpload(session, terminal, socket, controller.signal)
      .catch(error => handleUploadError(error, session))
      .finally(onFinished);
  };

  const handleFileChange = (options: { fileList: UploadFileInfo[] }) => {
    fileInfo.value = (options.fileList[0]?.file as File) || null;
  };

  const handleSendSession = (session: ZmodemSession, terminal: Terminal, socket: WebSocket) => {
    activeSession.value = session;
    fileInfo.value = null;

    let uploadStarted = false;
    const dialog = modal.create({
      preset: 'dialog',
      title: t('UploadTitle'),
      showIcon: false,
      closable: false,
      closeOnEsc: false,
      maskClosable: false,
      positiveText: t('Upload'),
      negativeText: t('Cancel'),
      negativeButtonProps: {
        type: 'tertiary',
      },
      positiveButtonProps: {
        type: 'tertiary',
      },
      onPositiveClick: () => {
        if (!fileInfo.value) {
          message.error(t('MustSelectOneFile'));
          return false;
        }
        if (fileInfo.value.size >= MAX_TRANSFER_SIZE) {
          const content = `${t('ExceedTransferSize')}: ${formatTransferSize(MAX_TRANSFER_SIZE)}`;
          message.error(content);
          return false;
        }
        if (uploadStarted) {
          return false;
        }

        uploadStarted = true;
        dialog.positiveButtonProps = {
          type: 'tertiary',
          disabled: true,
          loading: true,
        };
        startUpload(session, terminal, socket, () => dialog.destroy());
        return false;
      },
      onNegativeClick: () => {
        cancelSendSession(session, socket);
        return true;
      },
      content: () =>
        h(ZmodemUpload, {
          t,
          onFileChange: handleFileChange,
        }),
    });

    session.on('session_end', () => {
      dialog.destroy();
      handleSessionEnd(session, terminal);
    });
  };

  const handleReceiveSession = (session: ZmodemSession, terminal: Terminal) => {
    activeSession.value = session;

    session.on('offer', (transfer: Transfer) => {
      const buffer: Uint8Array[] = [];
      const detail = transfer.get_details();
      let previousPercent = -1;

      if (detail.size >= MAX_TRANSFER_SIZE) {
        const content = `${t('ExceedTransferSize')}: ${formatTransferSize(MAX_TRANSFER_SIZE)}`;
        message.info(content);
        transfer.skip();
        return;
      }

      transfer.on('input', (payload: Uint8Array) => {
        previousPercent = terminalProgress(transfer, terminal, previousPercent);
        buffer.push(new Uint8Array(payload));
      });

      transfer
        .accept()
        .then(() => {
          ZmodemBrowser.Browser.save_to_disk(buffer, detail.name);
          message.success(`${t('DownloadSuccess')}: ${detail.name}`);
          terminal.write('\r\n');
        })
        .catch((error: Error) => {
          message.error(`Error: ${error}`);
          abortSession(session);
        });
    });

    session.on('session_end', () => {
      if (session.aborted()) {
        terminal.write(`\r\n${t('ZmodemDownloadFailed')}\r\n`);
      }
      handleSessionEnd(session, terminal);
    });

    session.start();
  };

  const createSentry = (terminal: Terminal, socket: WebSocket, lastSendTime: Ref<Date>) => {
    const sentry = new ZmodemBrowser.Sentry({
      to_terminal: (octets: number[] | Uint8Array) => {
        if (draining.value) {
          // 不以固定延迟恢复：先丢弃残余二进制块，直到远端返回可读错误或 shell 提示符。
          if (!isReadableTerminalText(octets)) {
            return;
          }
          stopDraining();
        }
        try {
          if (!sentry.get_confirmed_session()) {
            terminal.write(octets instanceof Uint8Array ? octets : new Uint8Array(octets));
          }
        }
        catch (_error) {
          message.error(t('Failed to write to terminal'));
        }
      },
      sender: (octets: number[] | Uint8Array) => {
        if (socket.readyState !== WebSocket.OPEN) {
          throw new Error(t('WebSocket connection is closed, please refresh the page'));
        }
        lastSendTime.value = new Date();
        socket.send(new Uint8Array(octets));
      },
      on_retract: () => {},
      on_detect: (detection: Detection) => {
        try {
          const session = detection.confirm();

          terminal.write('\r\n');
          if (session.type === 'send') {
            handleSendSession(session, terminal, socket);
          }
          else {
            handleReceiveSession(session, terminal);
          }
        }
        catch (error) {
          console.warn('Error in ZMODEM detection:', error);
          abortSession(activeSession.value);
        }
      },
    });

    sentryRef.value = sentry;
    return sentry;
  };

  return {
    createSentry,
    cleanupSession,
    abortActiveSession,
    isActiveSession,
    finishDraining,
    stopDraining,
  };
};
