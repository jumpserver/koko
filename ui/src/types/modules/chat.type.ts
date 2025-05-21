// 定义会话消息类型
export type ChatMessage = ChatSendMessage | ChatReceiveMessage;

// 定义发送消息类型
export interface ChatSendMessage {
  data: string;

  id: string;

  prompt: string;
}

// 定义接收消息类型
export interface ChatReceiveMessage {
  chat_model: string;

  data: string;

  id: string;

  interrupt: boolean;

  prompt: string;

  type: string;
}

// 定义会话状态类型
export interface ChatState {
  // 会话角色
  prompt: string;

  // 会话消息
  // 数组的奇数项(index % 2 === 1)为收到的消息(ChatReceiveMessage)
  // 数组的偶数项(index % 2 === 0)为发出去的消息(ChatSendMessage)
  messages: ChatMessage[];
}

// 侧边栏
export interface ChatSider {
  time_stamp: string;

  chat_items: {
    id: string;

    chat_title: string;
  };
}

// 角色类型
export interface RoleType {
  content: string;

  name: string;
}
