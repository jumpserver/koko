import mitt, { Emitter } from 'mitt';

type Event = {
  updateTreeNodes: {
    key: string;
    label: string;
    isLeaf: boolean;
  };
};

const mittBus: Emitter<Event> = mitt();

export default mittBus;
