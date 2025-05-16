import { defineStore } from 'pinia';
import type { ChatState, ChatMessage } from '@/types/modules/chat.type';

type Chatitem = {
  chatItem: Map<string, ChatState>;
};

export const useChatStore = defineStore('chat', {
  state: (): Chatitem => ({
    chatItem: new Map<string, ChatState>()
  }),
  actions: {
    addChatItem(id: string, chatState: ChatState) {
      this.chatItem.set(id, chatState);
    },
    removeChatItem(id: string) {
      this.chatItem.delete(id);
    },
    addMessageContext(id: string, message: ChatMessage) {
      const chatState = this.chatItem.get(id);

      if (chatState) {
        chatState.messages.push(message);
      }
    }
  }
});
