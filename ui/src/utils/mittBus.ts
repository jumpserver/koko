import mitt, { Emitter } from 'mitt';
import { shareUser } from '@/views/interface';

type Event = {
  'open-setting': void;
  'show-theme-config': void;
  'set-theme': { themeName: string };
  'share-user': { type: string; query: string };
  'sync-theme': { type: string; data: any };
  'create-share-url': {
    type: string;
    sessionId: string;
    shareLinkRequest: { expiredTime: number; actionPerm: string; users: shareUser[] };
  };
};

const mittBus: Emitter<Event> = mitt();

export default mittBus;
