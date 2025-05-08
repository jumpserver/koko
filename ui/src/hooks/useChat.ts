import { ref } from 'vue';
import { MessageType } from '@/enum';
import { useWebSocket } from '@vueuse/core';
import { generateWsURL } from '@/hooks/helper';

export const useChat = () => {
  const socket = ref<WebSocket>();

  const socketOnMessage = (message: MessageEvent) => {
    const messageData = JSON.parse(message.data);

    console.log(messageData);

    switch (messageData.type) {
      case MessageType.CONNECT:
        break;
    }
  };

  const createChatSocket = () => {
    const url = generateWsURL();

    const { ws } = useWebSocket(url);

    if (!ws.value) {
      return;
    }

    ws.value.onopen = () => {
      console.log('Connected to websocket');
    };
    ws.value.onmessage = socketOnMessage;
    ws.value.onclose = () => {
      console.log('Disconnected from websocket');
    };

    return {
      socket: socket.value
    };
  };

  return {
    createChatSocket
  };
};
