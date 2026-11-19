type ControlStatus = 'connecting' | 'open' | 'closed' | 'error';

export class ControlSocket {
  private ws: WebSocket | null = null;
  private closedByUser = false;

  constructor(
    private readonly url: string,
    private readonly token: string,
    private readonly onStatus: (status: ControlStatus) => void,
    private readonly onError?: (message: string) => void,
  ) {}

  connect() {
    this.closedByUser = false;
    this.onStatus('connecting');
    const ws = new WebSocket(this.url, [`ppclass.control.${this.token}`]);
    this.ws = ws;
    ws.onopen = () => this.onStatus('open');
    ws.onerror = () => {
      this.onStatus('error');
      this.onError?.('控制通道连接失败');
    };
    ws.onclose = () => {
      if (this.ws === ws) this.ws = null;
      this.onStatus('closed');
      if (!this.closedByUser) this.onError?.('控制通道已断开');
    };
  }

  send(event: Record<string, unknown>) {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify(event));
  }

  close() {
    this.closedByUser = true;
    this.ws?.close();
    this.ws = null;
    this.onStatus('closed');
  }
}
