import mitt from 'mitt';

type Event = {
  'open-setting': void;
  'show-theme-config': void;
  'set-theme': { themeName: string };
};

const mittBus = mitt<Event>();

export default mittBus;
