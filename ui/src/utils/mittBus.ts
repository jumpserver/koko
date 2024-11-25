import mitt, { Emitter } from 'mitt';
import { shareUser } from '@/views/interface';
import { customTreeOption } from '@/hooks/interface';
import { ManageTypes } from '@/hooks/useFileManage.ts';

type Event = {
  'remove-event': void;
  'alt-shift-right': void;
  'alt-shift-left': void;
  'open-setting': void;
  'open-fileList': void;
  'fold-tree-click': void;
  'show-theme-config': void;
  'set-Terminal-theme': string;
  'connect-terminal': customTreeOption;
  'set-theme': { themeName: string };
  'file-manage': { path: string; type: ManageTypes; new_name?: string };
  'download-file': { path: string; is_dir: boolean };
  'terminal-search': { keyword: string; type?: string };
  'share-user': { type: string; query: string };
  'sync-theme': { type: string; data: any };
  'remove-share-user': { sessionId: string; userMeta: any; type: string };
  'create-share-url': {
    type: string;
    sessionId: string;
    shareLinkRequest: { expiredTime: number; actionPerm: string; users: shareUser[] };
  };
};

const mittBus: Emitter<Event> = mitt();

export default mittBus;
