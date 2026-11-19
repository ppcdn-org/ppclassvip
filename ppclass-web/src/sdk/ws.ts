import type { WsEvent } from './types';

type Status = 'connecting' | 'open' | 'closed' | 'reconnecting';

interface Options {
  url: string;
  token: string;
  onEvent: (event: WsEvent) => void;
  onStatus?: (status: Status) => void;
  onClose?: (code: number, reason: string) => void;
}

const RECONNECT_DELAYS = [1000, 2000, 5000, 10000];
const TERMINAL_CLOSE_REASONS = new Set(['room_ended', 'room_deleted']);

export class ClassroomWebSocket {
  private ws: WebSocket | null = null;
  private closedByUser = false;
  private reconnectAttempt = 0;
  private reconnectTimer: number | null = null;
  private queue: string[] = [];

  constructor(private readonly options: Options) {}

  get protocol() {
    return `ppclass.token.${this.options.token}`;
  }

  connect() {
    this.closedByUser = false;
    this.clearTimer();
    this.options.onStatus?.('connecting');
    const ws = new WebSocket(this.options.url, [this.protocol]);
    this.ws = ws;

    ws.onopen = () => {
      this.reconnectAttempt = 0;
      this.options.onStatus?.('open');
      for (const payload of this.queue.splice(0)) ws.send(payload);
    };

    ws.onmessage = (message) => {
      try {
        const event = JSON.parse(String(message.data)) as WsEvent;
        if (event && typeof event.type === 'string') this.options.onEvent(event);
      } catch {
        // Ignore malformed frames; the server only sends JSON.
      }
    };

    ws.onerror = () => {
      // onclose handles reconnection.
    };

    ws.onclose = (event) => {
      if (this.ws === ws) this.ws = null;
      this.options.onClose?.(event.code, event.reason);
      if (this.closedByUser) {
        this.options.onStatus?.('closed');
        return;
      }
      if (TERMINAL_CLOSE_REASONS.has(event.reason)) {
        this.closedByUser = true;
        this.options.onStatus?.('closed');
        return;
      }
      this.options.onStatus?.('reconnecting');
      const delay = RECONNECT_DELAYS[Math.min(this.reconnectAttempt, RECONNECT_DELAYS.length - 1)];
      this.reconnectAttempt += 1;
      this.reconnectTimer = window.setTimeout(() => this.connect(), delay);
    };
  }

  send(type: string, data: unknown = {}) {
    const payload = JSON.stringify({ type, data });
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(payload);
      return;
    }
    if (this.queue.length < 100) this.queue.push(payload);
  }

  close() {
    this.closedByUser = true;
    this.clearTimer();
    this.queue = [];
    this.ws?.close();
    this.ws = null;
    this.options.onStatus?.('closed');
  }

  private clearTimer() {
    if (this.reconnectTimer != null) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }
}
