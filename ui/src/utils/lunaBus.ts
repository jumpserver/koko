import type { Emitter } from 'mitt';
import type { LunaMessage, LunaMessageEvents } from '@/types/modules/postmessage.type';
import mitt from 'mitt';
import { LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';

// 获取所有事件类型
export type LunaEventType = keyof LunaMessageEvents;

// 创建事件-数据映射类型
type EventPayloadMap = {
  [K in LunaEventType]: LunaMessageEvents[K]['data'] extends undefined ? void : LunaMessageEvents[K]['data'];
};

const allEventTypes = Object.keys(LUNA_MESSAGE_TYPE) as LunaEventType[];

class LunaCommunicator<T extends EventPayloadMap = EventPayloadMap> {
  private mitt: Emitter<T>;
  private lunaId: string = '';
  private targetOrigin: string = '*';
  private protocol: string = '';

  constructor() {
    this.mitt = mitt<T>();
    this.setupMessageListener();
  }

  private setupMessageListener() {
    window.addEventListener('message', (event: MessageEvent) => {
      const message: LunaMessage = event.data;
      switch (message.name) {
        case LUNA_MESSAGE_TYPE.PING:
          this.lunaId = message.id;
          this.targetOrigin = event.origin;
          this.protocol = message.protocol;
          this.sendLuna(LUNA_MESSAGE_TYPE.PONG, '');
          break;
        default:
          // 处理其他类型的消息
          if (allEventTypes.includes(message.name as LunaEventType)) {
            const eventType = message.name as keyof T;
            const data = message as T[keyof T];
            this.mitt.emit(eventType, data);
          }
          else {
            console.warn(`Unhandled message type: ${message.name}`, message);
          }
      }
    });
  }

  // 发送消息到目标窗口
  public sendLuna<K extends keyof T>(name: K, data: T[K]) {
    if (!this.lunaId || !this.targetOrigin) {
      console.warn('Target window not set');
    }

    window.parent.postMessage({ name, id: this.lunaId, data }, this.targetOrigin);
  }

  // 监听事件
  public onLuna<K extends keyof T>(type: K, handler: (data: T[K]) => void) {
    this.mitt.on(type, handler);
  }

  // 移除监听器
  public offLuna<K extends keyof T>(type: K, handler?: (data: T[K]) => void) {
    this.mitt.off(type, handler);
  }

  // 监听一次性事件
  public once<K extends keyof T>(type: K, handler: (data: T[K]) => void) {
    const onceHandler = (data: T[K]) => {
      handler(data);
      this.offLuna(type, onceHandler);
    };
    this.onLuna(type, onceHandler);
  }

  // 销毁实例
  public destroy() {
    this.mitt.all.clear();
  }

  // 获取所有事件类型
  public getEventTypes(): Array<keyof T> {
    return Object.keys(this.mitt.all) as Array<keyof T>;
  }
}

export const lunaCommunicator = new LunaCommunicator<EventPayloadMap>();
