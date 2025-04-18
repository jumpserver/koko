export interface OnlineUser {
  user_id: string;
  user: string;
  created: string;
  remote_addr: string;
  terminal_id: string;
  primary: boolean;
  writable: boolean;
}

export interface ShareUserOptions {
  id: string;

  name: string;

  username: string;
}
