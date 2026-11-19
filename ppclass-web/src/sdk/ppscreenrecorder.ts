export type RecorderState = 'idle' | 'recording' | 'stopped' | 'error';

interface Options {
  onState?: (state: RecorderState, detail?: string) => void;
  onError?: (error: Error) => void;
  chunkIntervalMs?: number;
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

// ScreenRecorder captures the screen plus mixed audio in the browser. The
// resulting WebM stays on the teacher's machine: stop() returns a Blob that the
// caller saves as a local file, and nothing is uploaded to the server.
export class ScreenRecorder {
  private recorder: MediaRecorder | null = null;
  private audioContext: AudioContext | null = null;
  private destination: MediaStreamAudioDestinationNode | null = null;
  private chunks: Blob[] = [];
  private mimeType = 'video/webm';
  private lastError: Error | null = null;
  private state: RecorderState = 'idle';

  constructor(private readonly options: Options) {}

  get currentState() {
    return this.state;
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
    this.mimeType = mimeType;
    this.chunks = [];
    const mixed = new MediaStream([videoTrack, ...destination.stream.getAudioTracks()]);
    const recorder = new MediaRecorder(mixed, { mimeType });
    this.recorder = recorder;
    this.lastError = null;
    this.setState('recording');

    recorder.ondataavailable = (event) => {
      if (event.data.size > 0) this.chunks.push(event.data);
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
    if (!recorder) {
      return new Blob(this.chunks, { type: this.mimeType });
    }
    await new Promise<void>((resolve) => {
      recorder.onstop = () => resolve();
      recorder.stop();
    });
    this.recorder = null;
    this.setState('stopped');
    this.cleanup();
    const blob = new Blob(this.chunks, { type: this.mimeType });
    this.chunks = [];
    if (this.lastError) throw this.lastError;
    return blob;
  }

  abort() {
    try {
      this.recorder?.stop();
    } catch {
      // ignore
    }
    this.recorder = null;
    this.chunks = [];
    this.cleanup();
    this.setState('stopped');
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
