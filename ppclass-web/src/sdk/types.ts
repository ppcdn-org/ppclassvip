export type Role = 'teacher' | 'student' | 'admin';

export interface User {
  id: number;
  email: string;
  nickname: string;
  role: Role;
  status: string;
}

export interface Classroom {
  id: number;
  roomCode: string;
  title: string;
  teacherUserId: number;
  state: string;
  recordingState: string;
  screenOwnerParticipantId?: number;
  screenLeg?: string;
  chatEnabled: boolean;
  studentScreenAllowed: boolean;
  endReason?: string;
  startedAt?: string;
  endedAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface Participant {
  id: number;
  roomId: number;
  userId?: number;
  displayName: string;
  role: Role;
  guest: boolean;
  muted: boolean;
  joinedAt: string;
  lastSeenAt: string;
  leftAt?: string;
}

export interface Attachment {
  id: number;
  roomId: number;
  uploaderParticipantId: number;
  kind: 'image' | 'file';
  originalName: string;
  contentType: string;
  size: number;
  status: string;
  createdAt: string;
}

export interface ChatMessage {
  id: number;
  roomId: number;
  senderParticipantId: number;
  senderRole: string;
  type: 'text' | 'image' | 'file' | 'system';
  body: string;
  attachmentId?: number;
  attachment?: Attachment;
  clientMsgId: string;
  createdAt: string;
}

export interface MediaCredential {
  url: string;
  bearerToken?: string;
  codec?: string;
  streamPath: string;
  expiresAt: string;
}

export interface MediaBundle {
  enabled: boolean;
  publish?: Record<string, MediaCredential>;
  subscribe?: Record<string, MediaCredential>;
  iceServers?: RTCIceServer[];
  expiresAt?: string;
}

export interface JoinResult {
  room: Classroom;
  participant: Participant;
  roomToken: string;
  expiresAt: string;
  ws: { path: string; protocolPrefix: string };
  media: MediaBundle;
}

export interface Recording {
  id: number;
  roomId: number;
  recordingId: string;
  uploaderParticipantId: number;
  includesStudentAudio: boolean;
  consentAt?: string;
  consentBy?: number;
  startedAt: string;
  endedAt?: string;
  status: string;
  error?: string;
}

export type RemoteDesktopMode = 'view_only' | 'control';

export interface RemoteDesktopSession {
  id: number;
  sessionId: string;
  roomId: number;
  teacherParticipantId: number;
  studentParticipantId: number;
  mode: RemoteDesktopMode;
  status: string;
  consentAt?: string;
  startedAt?: string;
  endedAt?: string;
  expiresAt?: string;
  endReason?: string;
}
export interface WsEvent<T = unknown> {
  type: string;
  data: T;
}

export interface AuditEntry {
  id: number;
  roomId: number;
  participantId: number;
  userId: number;
  action: string;
  detail: string;
  clientIp: string;
  createdAt: string;
}

export interface RoomSession {
  roomId: number;
  roomToken: string;
  participantId: number;
  role: Role;
  displayName: string;
  expiresAt: string;
}
