import mitt, { Emitter } from 'mitt';
import { ManageTypes } from '@/hooks/useFileManage.ts';

import type { Ref } from 'vue';
import type { ShareUserOptions } from '@/types/modules/user.type';
import type { UploadFileInfo } from 'naive-ui';
import type { customTreeOption } from '@/hooks/interface';

type Event = {
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
  'writeDataToTerminal': { type: string };
};

const mittBus: Emitter<Event> = mitt();

export default mittBus;
