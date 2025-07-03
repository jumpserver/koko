import { onUnmounted } from 'vue';

import type { LunaEventType } from '@/utils/lunaBus';
import type { TerminalSessionInfo } from '@/types/modules/postmessage.type';

import { useTerminalContext } from '@/context/terminalContext';

/**
 * 替代原来的 sendLunaEvent 和 eventBus，同时提供 Luna 通信功能
 */
export const useTerminalEvents = () => {
  const context = useTerminalContext();

  /**
   * 发送 Luna 事件
   * @param {string} event - 事件名称
   * @param {any} data - 事件数据
   */
  const sendLunaEvent = (event: string, data: any) => {
    context.sendLunaEvent(event, data);
  };

  /**
   * 监听终端会话事件
   * @param {Function} callback - 事件处理函数
   */
  const onTerminalSession = (callback: (info: TerminalSessionInfo) => void) => {
    context.eventBus.on('terminal-session', callback);

    onUnmounted(() => {
      context.eventBus.off('terminal-session', callback);
    });

    return () => context.eventBus.off('terminal-session', callback);
  };

  /**
   * 监听终端连接事件
   * @param {Function} callback - 事件处理函数
   */
  const onTerminalConnect = (callback: (data: { id: string }) => void) => {
    context.eventBus.on('terminal-connect', callback);

    onUnmounted(() => {
      context.eventBus.off('terminal-connect', callback);
    });

    return () => context.eventBus.off('terminal-connect', callback);
  };

  /**
   * 监听 Luna 事件 - 用于组件间通信
   * @param {Function} callback - 事件处理函数
   */
  const onLunaEvent = (callback: (data: { event: string; data: any }) => void) => {
    context.eventBus.on('luna-event', callback);

    onUnmounted(() => {
      context.eventBus.off('luna-event', callback);
    });

    return () => context.eventBus.off('luna-event', callback);
  };

  /**
   * 触发终端会话事件
   * @param {TerminalSessionInfo} info - 终端会话信息
   */
  const emitTerminalSession = (info: TerminalSessionInfo) => {
    context.eventBus.emit('terminal-session', info);
  };

  /**
   * 触发终端连接事件
   * @param {string} id - 终端 ID
   */
  const emitTerminalConnect = (id: string) => {
    context.eventBus.emit('terminal-connect', { id });
  };

  /**
   * 发送消息到 Luna（父窗口）
   * @param name - 事件名称
   * @param data - 事件数据
   */
  const sendToLuna = <K extends LunaEventType>(name: K, data: any) => {
    context.lunaCommunicator.sendLuna(name, data);
  };

  /**
   * 监听来自 Luna 的消息
   * @param type - 事件类型
   * @param handler - 事件处理函数
   * @returns
   */
  const onLunaMessage = <K extends LunaEventType>(type: K, handler: (data: any) => void) => {
    context.lunaCommunicator.onLuna(type, handler);

    onUnmounted(() => {
      context.lunaCommunicator.offLuna(type, handler);
    });

    return () => context.lunaCommunicator.offLuna(type, handler);
  };

  /**
   * 监听一次性 Luna 消息
   * @param type - 事件类型
   * @param handler - 事件处理函数
   */
  const onLunaMessageOnce = <K extends LunaEventType>(type: K, handler: (data: any) => void) => {
    context.lunaCommunicator.once(type, handler);
  };

  return {
    sendLunaEvent,
    emitTerminalSession,
    emitTerminalConnect,
    onTerminalSession,
    onTerminalConnect,
    onLunaEvent,

    sendToLuna,
    onLunaMessage,
    onLunaMessageOnce,

    lunaCommunicator: context.lunaCommunicator,
  };
};
