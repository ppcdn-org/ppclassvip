import type { MediaCredential } from './types';

interface ReaderConfig {
  url: string;
  videoCodec?: string;
  insertableStreams?: boolean;
  onTrack?: (event: RTCTrackEvent) => void;
  onConnected?: () => void;
  onError?: (message: string, status?: number) => void;
  onUnauthorized?: (message: string) => void;
}

interface ReaderInstance {
  close(): void;
}

interface ReaderConstructor {
  new (conf: ReaderConfig): ReaderInstance;
}

type VendorModules = {
  whipPublish: (
    decision: { whipUrl: string; bearerToken?: string },
    pc: RTCPeerConnection,
    options?: { iceGatheringTimeoutMs?: number },
  ) => Promise<string>;
  preferH264: (transceiver: RTCRtpTransceiver) => void;
  MediaMTXWebRTCReader: ReaderConstructor;
};

let vendorPromise: Promise<VendorModules> | null = null;

function loadVendor() {
  vendorPromise ??= Promise.all([
    import(/* @vite-ignore */ `${'/vendor/pplayer'}/ppwebpublish.mjs`),
    import(/* @vite-ignore */ `${'/vendor/pplayer'}/ppplayer.mjs`),
  ]).then(([publish, player]) => ({
    whipPublish: publish.whipPublish,
    preferH264: publish.preferH264,
    MediaMTXWebRTCReader: player.MediaMTXWebRTCReader,
  }));
  return vendorPromise;
}

const DEFAULT_STUN_SERVERS: RTCIceServer[] = [
  { urls: ['stun:stun.l.google.com:19302', 'stun:stun1.l.google.com:19302'] },
];

function whepUrl(base: string) {
  const url = new URL(base, window.location.origin);
  if (!url.pathname.endsWith('/whep')) {
    url.pathname = `${url.pathname.replace(/\/$/, '')}/h264/whep`;
  }
  return url.toString();
}

type StreamListener = (leg: string, stream: MediaStream | null) => void;
type ErrorListener = (message: string) => void;

export class ClassroomMedia {
  private cameraStream: MediaStream | null = null;
  private screenStream: MediaStream | null = null;
  private cameraPC: RTCPeerConnection | null = null;
  private screenPC: RTCPeerConnection | null = null;
  private readers = new Map<
    string,
    { reader: { close(): void }; stream: MediaStream; audio?: MediaStreamTrack; closed?: boolean }
  >();

  constructor(
    private readonly onStream: StreamListener,
    private readonly onError: ErrorListener,
  ) {}

  getLocalCameraStream() {
    return this.cameraStream;
  }

  getScreenStream() {
    return this.screenStream;
  }

  getRemoteAudioTrack() {
    for (const entry of this.readers.values()) {
      if (entry.audio) return entry.audio;
    }
    return null;
  }

  getSubscribedLegs() {
    return [...this.readers.keys()];
  }

  async republishCamera(credential: MediaCredential, iceServers: RTCIceServer[] = []) {
    if (!this.cameraStream) return;
    this.cameraPC?.close();
    this.cameraPC = await this.publish(this.cameraStream, credential, iceServers);
  }

  async republishScreen(credential: MediaCredential, iceServers: RTCIceServer[] = []) {
    if (!this.screenStream) return;
    this.screenPC?.close();
    this.screenPC = await this.publish(this.screenStream, credential, iceServers);
  }

  async publishCamera(credential: MediaCredential, iceServers: RTCIceServer[] = []) {
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      video: { width: { ideal: 1280 }, height: { ideal: 720 }, frameRate: { ideal: 30 } },
    });
    this.stopCameraTracks();
    this.cameraStream = stream;
    this.cameraPC = await this.publish(stream, credential, iceServers);
    return stream;
  }

  async publishScreen(credential: MediaCredential, iceServers: RTCIceServer[] = []) {
    const stream = await navigator.mediaDevices.getDisplayMedia({
      video: {
        width: { ideal: 1920 },
        height: { ideal: 1080 },
        frameRate: { ideal: 15 },
      },
      audio: false,
    });
    const video = stream.getVideoTracks()[0];
    if (video) video.contentHint = 'text';
    this.stopScreenTracks();
    this.screenStream = stream;
    this.screenPC = await this.publish(stream, credential, iceServers);
    return stream;
  }

  subscribe(leg: string, credential: MediaCredential) {
    this.stopSubscribe(leg);
    const entry: {
      reader: { close(): void };
      stream: MediaStream;
      audio?: MediaStreamTrack;
      closed?: boolean;
    } = {
      reader: { close: () => undefined },
      stream: new MediaStream(),
    };
    this.readers.set(leg, entry);
    loadVendor()
      .then(({ MediaMTXWebRTCReader }) => {
        if (entry.closed) return;
        const reader = new MediaMTXWebRTCReader({
          url: whepUrl(credential.url),
          videoCodec: credential.codec || 'h264',
          insertableStreams: false,
          onTrack: (event) => {
            if (entry.closed) return;
            const stream = event.streams[0] ?? new MediaStream([event.track]);
            entry.stream = stream;
            if (event.track.kind === 'audio') entry.audio = event.track;
            this.onStream(leg, stream);
          },
          onError: (message, status) => {
            // 404 means the edge has not registered the pipeline yet; the
            // reader retries the same WHEP URL internally, so keep it alive.
            if (status !== 404) this.onError(`${leg}: ${message}`);
            if (status === 401 || status === 403) this.stopSubscribe(leg);
          },
          onUnauthorized: (message) => {
            this.onError(`${leg}: ${message}`);
            this.stopSubscribe(leg);
          },
        });
        entry.reader = reader;
      })
      .catch((error: unknown) => {
        this.onError(error instanceof Error ? error.message : '媒体模块加载失败');
        this.stopSubscribe(leg);
      });
  }

  stopSubscribe(leg: string) {
    const entry = this.readers.get(leg);
    if (!entry) return;
    entry.closed = true;
    entry.reader.close();
    this.readers.delete(leg);
    this.onStream(leg, null);
  }

  setCameraEnabled(enabled: boolean) {
    for (const track of this.cameraStream?.getVideoTracks() ?? []) track.enabled = enabled;
  }

  setMicEnabled(enabled: boolean) {
    for (const track of this.cameraStream?.getAudioTracks() ?? []) track.enabled = enabled;
  }

  stopCamera() {
    this.cameraPC?.close();
    this.cameraPC = null;
    this.stopCameraTracks();
  }

  stopScreen() {
    this.screenPC?.close();
    this.screenPC = null;
    this.stopScreenTracks();
  }

  stopAll() {
    this.stopCamera();
    this.stopScreen();
    for (const leg of [...this.readers.keys()]) this.stopSubscribe(leg);
  }

  private async publish(
    stream: MediaStream,
    credential: MediaCredential,
    iceServers: RTCIceServer[],
  ) {
    const { whipPublish, preferH264 } = await loadVendor();
    const pc = new RTCPeerConnection({
      iceServers: iceServers.length > 0 ? iceServers : DEFAULT_STUN_SERVERS,
    });
    for (const track of stream.getTracks()) {
      const transceiver = pc.addTransceiver(track, {
        direction: 'sendonly',
        streams: [stream],
      });
      if (track.kind === 'video') preferH264(transceiver);
    }
    await whipPublish(
      { whipUrl: credential.url, bearerToken: credential.bearerToken },
      pc,
      { iceGatheringTimeoutMs: 3000 },
    );
    return pc;
  }

  private stopCameraTracks() {
    for (const track of this.cameraStream?.getTracks() ?? []) track.stop();
    this.cameraStream = null;
  }

  private stopScreenTracks() {
    for (const track of this.screenStream?.getTracks() ?? []) track.stop();
    this.screenStream = null;
  }
}

