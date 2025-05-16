import { ref } from 'vue';
import { MessageType } from '@/enum';
import { useMessage } from 'naive-ui';
import { useWebSocket } from '@vueuse/core';
import { generateWsURL } from '@/hooks/helper';

import type { ChatSendMessage } from '@/types/modules/chat.type';

export const useChat = () => {
  const message = useMessage();
  const socket = ref<WebSocket>();

  const socketOnMessage = (message: MessageEvent) => {
    let data = '';
    const messageData = JSON.parse(message.data);

    if (typeof messageData.data === 'string') {
      data = JSON.parse(messageData.data);
    }

    switch (messageData.type) {
      case MessageType.CONNECT:
        // console.log(data);
        break;
      case MessageType.MESSAGE:
        console.log(data);
        break;
    }
  };
  const socketClose = () => {
    message.error('Socket connection has been closed');
  };
  const socketError = () => {
    message.error('Socket connection has been error');
  };
  const socketOpen = () => {
    // TODO 发送心跳
  };

  const sendChatMessage = (message: ChatSendMessage) => {
    socket.value?.send(JSON.stringify(message));
  };

  const createChatSocket = () => {
    const url = generateWsURL();

    const { ws } = useWebSocket(url);

    if (!ws.value) {
      return;
    }

    socket.value = ws.value;

    ws.value.onopen = socketOpen;
    ws.value.onclose = socketClose;
    ws.value.onerror = socketError;
    ws.value.onmessage = socketOnMessage;

    return {
      socket: socket.value
    };
  };

  return {
    sendChatMessage,
    createChatSocket
  };
};
