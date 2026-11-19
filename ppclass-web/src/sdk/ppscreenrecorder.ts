export type RecorderState = 'idle' | 'recording' | 'stopped' | 'error';

interface Options {
  uploadChunk: (seq: number, chunk: Blob) => Promise<unknown>;
  onState?: (state: RecorderState, detail?: string) => void;
  onError?: (error: Error) => void;
  chunkIntervalMs?: number;
  retries?: number;
}

interface StartInput {
  screenStream: MediaStream;
  micStream: MediaStream;
  remoteAudioTrack?: MediaStreamTrack | null;
}

function pickMimeType() {
  const candidates = [
    'video/webm;codecs=vp9,opus',
    'video/webm;codecs=vp8,opus',
    'video/webm',
  ];
  return candidates.find((type) => MediaRecorder.isTypeSupported(type)) || null;
}

export class ScreenRecorder {
  private recorder: MediaRecorder | null = null;
  private audioContext: AudioContext | null = null;
  private destination: MediaStreamAudioDestinationNode | null = null;
  private uploadChain: Promise<void> = Promise.resolve();
  private nextSeq = 0;
  private lastError: Error | null = null;
  private state: RecorderState = 'idle';

  constructor(private readonly options: Options) {}

  get currentState() {
    return this.state;
  }

  get nextChunkSeq() {
    return this.nextSeq;
  }

  async start(input: StartInput) {
    if (this.recorder) throw new Error('recorder is already running');
    const mimeType = pickMimeType();
    if (!mimeType) throw new Error('当前浏览器不支持 WebM MediaRecorder，无法录屏');
    const videoTrack = input.screenStream.getVideoTracks()[0];
    if (!videoTrack) throw new Error('屏幕流没有视频轨');

    const audioContext = new AudioContext();
    await audioContext.resume();
    const destination = audioContext.createMediaStreamDestination();
    for (const track of input.micStream.getAudioTracks()) {
      audioContext.createMediaStreamSource(new MediaStream([track])).connect(destination);
    }
    if (input.remoteAudioTrack && input.remoteAudioTrack.readyState !== 'ended') {
      audioContext
        .createMediaStreamSource(new MediaStream([input.remoteAudioTrack]))
        .connect(destination);
    }

    this.audioContext = audioContext;
    this.destination = destination;
    const mixed = new MediaStream([videoTrack, ...destination.stream.getAudioTracks()]);
    const recorder = new MediaRecorder(mixed, { mimeType });
    this.recorder = recorder;
    this.nextSeq = 0;
    this.lastError = null;
    this.setState('recording');

    recorder.ondataavailable = (event) => {
      if (event.data.size > 0) this.enqueue(event.data);
    };
    recorder.onerror = (event) => {
      const error = new Error(`MediaRecorder error: ${String(event)}`);
      this.lastError = error;
      this.setState('error', error.message);
      this.options.onError?.(error);
    };
    recorder.start(this.options.chunkIntervalMs ?? 5000);
  }

  async stop() {
    const recorder = this.recorder;
    if (!recorder) return;
    await new Promise<void>((resolve) => {
      recorder.onstop = () => resolve();
      recorder.stop();
    });
    this.recorder = null;
    this.setState('stopped');

    try {
      await this.uploadChain;
    } catch {
      // The chunk upload already reported the error through onError.
    }
    this.cleanup();
    if (this.lastError) throw this.lastError;
  }

  abort() {
    try {
      this.recorder?.stop();
    } catch {
      // ignore
    }
    this.recorder = null;
    this.cleanup();
    this.setState('stopped');
  }

  private enqueue(chunk: Blob) {
    const seq = this.nextSeq++;
    this.uploadChain = this.uploadChain.then(async () => {
      if (this.lastError) return;
      await this.uploadWithRetry(seq, chunk);
    });
    this.uploadChain = this.uploadChain.catch((error: unknown) => {
      const err = error instanceof Error ? error : new Error(String(error));
      this.lastError = err;
      this.setState('error', err.message);
      this.options.onError?.(err);
      throw err;
    });
  }

  private async uploadWithRetry(seq: number, chunk: Blob) {
    const retries = this.options.retries ?? 4;
    let lastError: unknown;
    for (let attempt = 0; attempt <= retries; attempt += 1) {
      try {
        await this.options.uploadChunk(seq, chunk);
        return;
      } catch (error) {
        lastError = error;
        if (attempt === retries) break;
        await new Promise((resolve) => window.setTimeout(resolve, 500 * 2 ** attempt));
      }
    }
    throw lastError instanceof Error ? lastError : new Error('录屏分片上传失败');
  }

  private setState(state: RecorderState, detail?: string) {
    this.state = state;
    this.options.onState?.(state, detail);
  }

  private cleanup() {
    this.destination?.stream.getTracks().forEach((track) => track.stop());
    this.destination = null;
    void this.audioContext?.close();
    this.audioContext = null;
  }
}

export function isRecordingSupported() {
  return typeof MediaRecorder !== 'undefined' && pickMimeType() !== null;
}
