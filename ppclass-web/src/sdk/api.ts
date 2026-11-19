import type {
  Attachment,
  ChatMessage,
  Classroom,
  JoinResult,
  Participant,
  Recording,
  User,
} from './types';

export const API_BASE = (import.meta.env.VITE_PPCLASS_API_BASE || '/api/v1').replace(/\/$/, '');

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

interface Envelope<T> {
  code: number;
  message: string;
  error?: string;
  data: T;
}

export class PpclassApi {
  private token: string | null = null;

  constructor(private readonly baseUrl = API_BASE) {}

  setToken(token: string | null) {
    this.token = token;
  }

  getToken() {
    return this.token;
  }

  async login(email: string, password: string) {
    const data = await this.request<{ token: string; user: User }>('POST', '/auth/login', {
      email,
      password,
    });
    this.token = data.token;
    return data;
  }

  me() {
    return this.request<User>('GET', '/auth/me');
  }

  changePassword(currentPassword: string, newPassword: string) {
    return this.request<{ changed: boolean }>('POST', '/auth/change-password', {
      currentPassword,
      newPassword,
    });
  }

  listClassrooms() {
    return this.request<{ rooms: Classroom[] }>('GET', '/classrooms');
  }

  createClassroom(title: string) {
    return this.request<{ room: Classroom }>('POST', '/classrooms', { title });
  }

  createInvite(roomId: number) {
    return this.request<{
      inviteToken: string;
      expiresAt: string;
      roomCode: string;
      joinPath: string;
    }>('POST', `/classrooms/${roomId}/invite`, {});
  }

  teacherJoin(roomId: number, nickname?: string) {
    return this.request<JoinResult>(
      'POST',
      `/classrooms/${roomId}/join`,
      nickname?.trim() ? { nickname: nickname.trim() } : {},
    );
  }

  guestJoin(inviteToken: string, nickname: string) {
    return this.request<JoinResult>('POST', '/classrooms/join', { inviteToken, nickname });
  }

  getClassroom(roomId: number) {
    return this.request<{ room: Classroom; participants: Participant[]; myRole: string }>(
      'GET',
      `/classrooms/${roomId}`,
    );
  }

  leaveClassroom(roomId: number) {
    return this.request<{ left: boolean }>('POST', `/classrooms/${roomId}/leave`, {});
  }

  endClassroom(roomId: number) {
    return this.request<{ room: Classroom }>('POST', `/classrooms/${roomId}/end`, {});
  }

  deleteClassroom(roomId: number) {
    return this.request<{ deleted: boolean }>('DELETE', `/classrooms/${roomId}`);
  }

  control(
    roomId: number,
    action: 'mute' | 'screen.permission' | 'chat',
    payload: { participantId?: number; muted?: boolean; granted?: boolean; enabled?: boolean },
  ) {
    return this.request<{ ok: boolean }>('POST', `/classrooms/${roomId}/control`, {
      action,
      ...payload,
    });
  }

  refreshMedia(roomId: number, leg: string, kind?: 'publish' | 'subscribe') {
    return this.request<{ leg: string; kind: string; credential: import('./types').MediaCredential }>(
      'POST',
      `/classrooms/${roomId}/media/refresh`,
      { leg, kind },
    );
  }

  startScreen(roomId: number) {
    return this.request<{
      leg: string;
      publish: import('./types').MediaCredential;
      streamPath: string;
      expiresAt: string;
    }>('POST', `/classrooms/${roomId}/screen/start`, {});
  }

  stopScreen(roomId: number) {
    return this.request<{ stopped: boolean }>('POST', `/classrooms/${roomId}/screen/stop`, {});
  }

  listMessages(roomId: number, afterId = 0) {
    const query = afterId > 0 ? `?afterId=${afterId}&limit=100` : '?limit=100';
    return this.request<{ messages: ChatMessage[] }>('GET', `/classrooms/${roomId}/messages${query}`);
  }

  sendMessage(
    roomId: number,
    input: {
      clientMsgId: string;
      type: 'text' | 'image' | 'file';
      body?: string;
      attachmentId?: number;
    },
  ) {
    return this.request<{ message: ChatMessage }>(
      'POST',
      `/classrooms/${roomId}/messages`,
      input,
    );
  }

  async uploadAttachment(roomId: number, file: File) {
    const form = new FormData();
    form.append('file', file, file.name);
    return this.request<{ attachment: Attachment }>(
      'POST',
      `/classrooms/${roomId}/attachments`,
      form,
    );
  }

  getAttachment(roomId: number, attachmentId: number) {
    return this.request<{ attachment: Attachment }>(
      'GET',
      `/classrooms/${roomId}/attachments/${attachmentId}`,
    );
  }

  attachmentUrl(roomId: number, attachmentId: number) {
    return `${this.baseUrl}/classrooms/${roomId}/attachments/${attachmentId}/download?token=${encodeURIComponent(
      this.token || '',
    )}`;
  }

  startRecording(roomId: number, studentAudioAvailable: boolean) {
    return this.request<{
      recordingId: string;
      status: string;
      chunkSize: number;
      maxChunkBytes: number;
      maxRecordingBytes: number;
      consentExpiresAt: string;
    }>('POST', `/classrooms/${roomId}/recordings/start`, { studentAudioAvailable });
  }

  consentRecording(roomId: number, recordingId: string) {
    return this.request<{ recording: Recording }>(
      'POST',
      `/classrooms/${roomId}/recordings/${recordingId}/consent`,
      null,
    );
  }

  declineRecording(roomId: number, recordingId: string) {
    return this.request<{ declined: boolean }>(
      'POST',
      `/classrooms/${roomId}/recordings/${recordingId}/decline`,
      null,
    );
  }

  getRecording(roomId: number, recordingId: string) {
    return this.request<{ recording: Recording }>(
      'GET',
      `/classrooms/${roomId}/recordings/${recordingId}`,
    );
  }

  async uploadRecordingChunk(roomId: number, recordingId: string, seq: number, chunk: Blob) {
    const response = await fetch(
      `${this.baseUrl}/classrooms/${roomId}/recordings/${recordingId}/chunks/${seq}`,
      {
        method: 'PUT',
        headers: this.headers({ 'Content-Type': 'application/octet-stream' }),
        body: chunk,
      },
    );
    const body = await response.json().catch(() => null);
    if (!response.ok) {
      throw new ApiError(response.status, body?.error || '', body?.message || '上传录屏分片失败');
    }
    return body.data as { lastChunkSeq: number; receivedBytes: number; duplicate?: boolean };
  }

  finalizeRecording(roomId: number, recordingId: string) {
    return this.request<{ recording: Recording }>(
      'POST',
      `/classrooms/${roomId}/recordings/${recordingId}/finalize`,
      null,
    );
  }

  abortRecording(roomId: number, recordingId: string) {
    return this.request<{ aborted: boolean }>(
      'POST',
      `/classrooms/${roomId}/recordings/${recordingId}/abort`,
      null,
    );
  }

  listRecordings(roomId: number) {
    return this.request<{ recordings: Recording[] }>(
      'GET',
      `/classrooms/${roomId}/recordings`,
    );
  }

  recordingUrl(roomId: number, recordingId: string) {
    return `${this.baseUrl}/classrooms/${roomId}/recordings/${recordingId}/download?token=${encodeURIComponent(
      this.token || '',
    )}`;
  }

  listAudit(roomId: number, limit = 100) {
    return this.request<{ entries: import('./types').AuditEntry[] }>(
      'GET',
      `/classrooms/${roomId}/audit?limit=${limit}`,
    );
  }

  requestRemoteDesktop(roomId: number, mode: 'view_only' | 'control') {
    return this.request<{ session: import('./types').RemoteDesktopSession; expiresAt: string }>(
      'POST',
      `/classrooms/${roomId}/remote-desktop/request`,
      { mode },
    );
  }

  consentRemoteDesktop(roomId: number, sessionId: string, mode?: 'view_only' | 'control') {
    return this.request<{
      session: import('./types').RemoteDesktopSession;
      agentCode: string;
      agentCodeExpiresAt: string;
      controlWsPath: string;
      expiresAt: string;
    }>('POST', `/classrooms/${roomId}/remote-desktop/${sessionId}/consent`, mode ? { mode } : {});
  }

  declineRemoteDesktop(roomId: number, sessionId: string) {
    return this.request<{ declined: boolean }>(
      'POST',
      `/classrooms/${roomId}/remote-desktop/${sessionId}/decline`,
      {},
    );
  }

  stopRemoteDesktop(roomId: number, sessionId: string) {
    return this.request<{ stopped: boolean }>(
      'POST',
      `/classrooms/${roomId}/remote-desktop/${sessionId}/stop`,
      {},
    );
  }

  getRemoteDesktop(roomId: number, sessionId: string) {
    return this.request<{
      session: import('./types').RemoteDesktopSession;
      controllerToken?: string;
      agentCode?: string;
      controlWsPath?: string;
    }>('GET', `/classrooms/${roomId}/remote-desktop/${sessionId}`);
  }

  wsUrl(roomId: number) {
    return websocketUrl(this.baseUrl, roomId);
  }

  remoteControlUrl(roomId: number, sessionId: string) {
    return remoteControlUrl(this.baseUrl, roomId, sessionId);
  }

  private headers(extra: Record<string, string> = {}) {
    const headers: Record<string, string> = { ...extra };
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    return headers;
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const isForm = body instanceof FormData;
    const response = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers: this.headers(isForm ? {} : { 'Content-Type': 'application/json' }),
      body:
        body == null
          ? undefined
          : isForm
            ? body
            : JSON.stringify(body),
    });
    const payload = (await response.json().catch(() => null)) as Envelope<T> | null;
    if (!response.ok || !payload) {
      throw new ApiError(
        response.status,
        payload?.error || '',
        payload?.message || `请求失败（${response.status}）`,
      );
    }
    return payload.data;
  }
}

export function websocketUrl(baseUrl: string, roomId: number) {
  const url = new URL(baseUrl, window.location.origin);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.pathname = `${url.pathname.replace(/\/$/, '')}/classrooms/${roomId}/ws`;
  url.search = '';
  return url.toString();
}

export function remoteControlUrl(baseUrl: string, roomId: number, sessionId: string) {
  const url = new URL(baseUrl, window.location.origin);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.pathname = `${url.pathname.replace(/\/$/, '')}/classrooms/${roomId}/remote-desktop/${sessionId}/control`;
  url.search = '';
  return url.toString();
}
