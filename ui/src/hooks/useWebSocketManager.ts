import { ref, computed } from 'vue';
import { darkTheme, createDiscreteApi } from 'naive-ui';
import { useWebSocket } from '@vueuse/core';
import { generateWsURL } from '@/hooks/helper';

import type { ConfigProviderProps } from 'naive-ui';

export const useWebSocketManager = () => {
  let socketInstance = ref<WebSocket>();


  const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
    theme: darkTheme
  }));

  const { message } = createDiscreteApi(['message'], {
    configProviderProps: configProviderPropsRef
  });

  const sendMessage = () => {

  }

  const createSocket = (): WebSocket | '' => {
    const url: string = generateWsURL();

    const { ws } = useWebSocket(url, {
      protocols: ['JMS-KOKO'],
      autoReconnect: {
        retries: 5,
        delay: 3000
      }
    })

    const socket = ws.value;

    if (socket) {
      socketInstance.value = socket;

      return socket
    }

    message.error('Failed to create WebSocket connection');
    return ''
  }
  

  return {
    sendMessage,
    createSocket
  }
}
