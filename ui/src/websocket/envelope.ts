export const ENVELOPE_VERSION = 0x01;
export const ENVELOPE_TERMINAL_INPUT = 0x01;
export const ENVELOPE_TERMINAL_OUTPUT = 0x02;
export const ENVELOPE_TERMINAL_COMMAND = 0x03;
export const ENVELOPE_ERROR = 0x04;
export const ENVELOPE_CHAT = 0x05;
export const ENVELOPE_TERMINAL_CREATE = 0x06;
export const ENVELOPE_TERMINAL_CLOSE = 0x07;

const HEADER_SIZE = 6;
const MAX_PAYLOAD_SIZE = 10 * 1024 * 1024;
const encoder = new TextEncoder();
const decoder = new TextDecoder('utf-8', { fatal: true });

export interface Envelope {
  type: number;
  payload: Uint8Array;
}

export interface TerminalCommandEnvelope {
  terminalId?: number;
  command: string;
  params?: Record<string, unknown>;
  requestId?: string;
  timestamp?: number;
}

export function buildEnvelope(type: number, payload: Uint8Array): Uint8Array {
  if (payload.byteLength > MAX_PAYLOAD_SIZE) {
    throw new Error('WebSocket envelope payload is too large');
  }
  const frame = new Uint8Array(HEADER_SIZE + payload.byteLength);
  const view = new DataView(frame.buffer);
  frame[0] = ENVELOPE_VERSION;
  frame[1] = type;
  view.setUint32(2, payload.byteLength, false);
  frame.set(payload, HEADER_SIZE);
  return frame;
}

export function parseEnvelope(value: ArrayBuffer | Uint8Array): Envelope {
  const frame = value instanceof Uint8Array ? value : new Uint8Array(value);
  if (frame.byteLength < HEADER_SIZE || frame[0] !== ENVELOPE_VERSION) {
    throw new Error('Invalid WebSocket envelope');
  }
  const length = new DataView(frame.buffer, frame.byteOffset, frame.byteLength).getUint32(2, false);
  if (length > MAX_PAYLOAD_SIZE || frame.byteLength !== HEADER_SIZE + length) {
    throw new Error('Invalid WebSocket envelope length');
  }
  return { type: frame[1]!, payload: frame.slice(HEADER_SIZE) };
}

export function buildJSONEnvelope(type: number, value: unknown): Uint8Array {
  return buildEnvelope(type, encoder.encode(JSON.stringify(value)));
}

export function parseJSONPayload<T>(payload: Uint8Array): T {
  return JSON.parse(decoder.decode(payload)) as T;
}

export function buildTerminalInput(terminalId: number, value: string | Uint8Array): Uint8Array {
  if (!Number.isInteger(terminalId) || terminalId <= 0) {
    throw new Error('A valid terminalId is required');
  }
  const data = typeof value === 'string' ? encoder.encode(value) : value;
  const payload = new Uint8Array(4 + data.byteLength);
  new DataView(payload.buffer).setUint32(0, terminalId, false);
  payload.set(data, 4);
  return buildEnvelope(ENVELOPE_TERMINAL_INPUT, payload);
}

export function parseTerminalPayload(payload: Uint8Array) {
  if (payload.byteLength < 4) {
    throw new Error('Terminal envelope payload is too short');
  }
  return {
    terminalId: new DataView(payload.buffer, payload.byteOffset, payload.byteLength).getUint32(0, false),
    data: payload.slice(4),
  };
}

export function createRequestId(prefix = 'request') {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
