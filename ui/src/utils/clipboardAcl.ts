import type {
  ClipboardAccess,
  ClipboardDirection,
  ClipboardDirectionAccess,
  ClipboardPermission,
  ClipboardPolicy,
  ClipboardPolicyItem,
  ClipboardValidationResult,
} from '@/types/modules/clipboard.type';

const normalizeLimit = (value: unknown): number => {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
};

const hasAction = (permission: ClipboardPermission | null | undefined, action: ClipboardDirection): boolean => {
  if (!Array.isArray(permission?.actions)) {
    // Share and monitor sessions do not carry token permissions. Preserve their
    // legacy behavior when no clipboard policy is available.
    return true;
  }

  return permission.actions.includes('all') || permission.actions.includes(action);
};

const resolveDirectionAccess = (
  direction: ClipboardDirection,
  permission?: ClipboardPermission | null,
  item?: ClipboardPolicyItem | null,
): ClipboardDirectionAccess => {
  const policyAllows = typeof item?.enabled === 'boolean' ? item.enabled : true;
  const operationHasAcl = item?.acl_action !== null;

  return {
    // A policy may only reduce the permission granted by the connect token.
    enabled: hasAction(permission, direction) && policyAllows,
    // acl_action=null means this operation was not selected by any clipboard
    // ACL. Ignore limits left on the policy item in that case.
    textLimit: operationHasAcl ? normalizeLimit(item?.text_limit) : 0,
    fileSizeLimit: operationHasAcl ? normalizeLimit(item?.file_size_limit) : 0,
  };
};

export const createUnrestrictedClipboardAccess = (): ClipboardAccess => ({
  copy: {
    enabled: true,
    textLimit: 0,
    fileSizeLimit: 0,
  },
  paste: {
    enabled: true,
    textLimit: 0,
    fileSizeLimit: 0,
  },
});

export const resolveClipboardAccess = (
  permission?: ClipboardPermission | null,
  policy?: ClipboardPolicy | null,
): ClipboardAccess => ({
  copy: resolveDirectionAccess('copy', permission, policy?.copy),
  paste: resolveDirectionAccess('paste', permission, policy?.paste),
});

export const getClipboardTextLength = (text: string): number => Array.from(text).length;

export const validateClipboardText = (
  access: ClipboardAccess,
  direction: ClipboardDirection,
  text: string,
): ClipboardValidationResult => {
  const directionAccess = access[direction];

  if (!directionAccess.enabled) {
    return {
      allowed: false,
      reason: 'permission',
    };
  }

  if (directionAccess.textLimit > 0 && getClipboardTextLength(text) > directionAccess.textLimit) {
    return {
      allowed: false,
      reason: 'text_limit',
      limit: directionAccess.textLimit,
    };
  }

  return { allowed: true };
};
