import type { Ref } from 'vue';
import type { Emitter } from 'mitt';
import type { UploadFileInfo } from 'naive-ui';

import mitt from 'mitt';

import type { ManageTypes } from '@/hooks/useFileManage.ts';
import type { ShareUserOptions } from '@/types/modules/user.type';
import type { customTreeOption } from '@/types/modules/config.type';

interface Event {
  'remove-event': void;
  'alt-shift-right': void;
  'alt-shift-left': void;
  'open-setting': void;
  'reload-table': void;
  'open-fileList': void;
  'fold-tree-click': void;
  'show-theme-config': void;
  'set-Terminal-theme': string;
  'connect-terminal': customTreeOption;
  'set-theme': { themeName: string };
  'file-manage': { path: string; type: ManageTypes; new_name?: string };
  'file-upload': {
    uploadFileList: Ref<Array<UploadFileInfo>>;
    onFinish: () => void;
    onError: () => void;
    onProgress: (e: { percent: number }) => void;
    loadingMessage?: any;
  };
  'download-file': { path: string; is_dir: boolean; size: string };
  'stop-upload': { fileInfo: UploadFileInfo };
  'terminal-search': { keyword: string; type?: string };
  'share-user': { type: string; query: string };
  'sync-theme': { type: string; data: any };
  'remove-share-user': { sessionId: string; userMeta: any; type: string };
  'create-share-url': {
    type: string;
    sessionId: string;
    shareLinkRequest: {
      expiredTime: number;
      actionPerm: string;
      users: ShareUserOptions[];
    };
  };
  writeDataToTerminal: { type: string };
  writeCommand: { type: string };
}

// @ts-expect-error mittBus is not typed
const mittBus = mitt<Event>();

export default mittBus;
