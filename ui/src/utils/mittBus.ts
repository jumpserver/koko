import mitt from 'mitt';

type Event = {
  'open-setting': void;
  'show-theme-config': void;
  'set-theme': { themeName: string };
  'sync-theme': { type: string; data: any };
};

const mittBus = mitt<Event>();

export default mittBus;
