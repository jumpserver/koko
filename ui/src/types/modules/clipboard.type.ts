export type ClipboardDirection = 'copy' | 'paste';

export interface ClipboardPolicyItem {
  enabled?: boolean;
  action?: string;
  perm_allowed?: boolean;
  acl_action?: string | null;
  text_limit?: number;
  file_size_limit?: number;
}

export interface ClipboardPolicy {
  copy?: ClipboardPolicyItem | null;
  paste?: ClipboardPolicyItem | null;
}

export interface ClipboardPermission {
  actions?: string[];
}

export interface ClipboardDirectionAccess {
  enabled: boolean;
  textLimit: number;
  fileSizeLimit: number;
}

export interface ClipboardAccess {
  copy: ClipboardDirectionAccess;
  paste: ClipboardDirectionAccess;
}

export interface ClipboardValidationResult {
  allowed: boolean;
  reason?: 'permission' | 'text_limit';
  limit?: number;
}
