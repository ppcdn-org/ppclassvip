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

export interface User {
  id: number;
  email: string;
  nickname: string;
  role: 'teacher' | 'admin';
  status: 'active' | 'disabled';
  createdAt: string;
  updatedAt: string;
}

export interface Classroom {
  id: number;
  roomCode: string;
  title: string;
  teacherUserId: number;
  state: string;
  recordingState: string;
  createdAt: string;
  startedAt?: string;
  endedAt?: string;
  endReason?: string;
}

export interface Participant {
  id: number;
  displayName: string;
  role: string;
  muted: boolean;
  guest: boolean;
  joinedAt: string;
  lastSeenAt: string;
  leftAt?: string;
}

export interface Attachment {
  id: number;
  roomId: number;
  uploaderParticipantId: number;
  kind: string;
  originalName: string;
  contentType: string;
  size: number;
  status: string;
  createdAt: string;
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

export interface Stats {
  users: number;
  teachers: number;
  admins: number;
  rooms: number;
  openRooms: number;
  endedRooms: number;
  attachments: number;
  attachmentBytes: number;
}

export interface Settings {
  maxFileBytes: number;
  maxImageBytes: number;
  roomQuotaBytes: number;
  recordingEnabled: boolean;
  consentTtlSeconds: number;
  idleTtlSeconds: number;
  attachmentRetentionDays: number;
  messageRatePerMinute: number;
  uploadRatePerMinute: number;
  inviteRatePerMinute: number;
  maxRoomsPerTeacher: number;
  updatedAt?: string;
}

export interface RoomWithTeacher {
  room: Classroom;
  teacher?: { id: number; email: string; nickname: string };
}

export interface TeacherOption {
  id: number;
  email: string;
  nickname: string;
}

interface Envelope<T> {
  code: number;
  message: string;
  error?: string;
  data: T;
}

export class AdminApi {
  private token: string | null = null;

  constructor(private readonly baseUrl = API_BASE) {}

  setToken(token: string | null) {
    this.token = token;
  }

  async login(email: string, password: string) {
    const data = await this.request<{ token: string; user: User }>('POST', '/auth/login', {
      email,
      password,
    });
    if (data.user.role !== 'admin') {
      throw new ApiError(403, 'admin_required', '该账号不是管理员');
    }
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

  dashboard() {
    return this.request<{ stats: Stats; recentRooms: RoomWithTeacher[] }>('GET', '/admin/dashboard');
  }

  listUsers(params: { query?: string; role?: string; status?: string; limit?: number; offset?: number }) {
    return this.request<{ users: User[]; total: number; limit: number; offset: number }>(
      'GET',
      `/admin/users${queryString(params)}`,
    );
  }

  createUser(input: { email: string; password: string; nickname: string; role: string }) {
    return this.request<{ user: User }>('POST', '/admin/users', input);
  }

  updateUser(
    id: number,
    input: { nickname?: string; role?: string; status?: string; password?: string },
  ) {
    return this.request<{ user: User }>('PUT', `/admin/users/${id}`, input);
  }

  listClassrooms(params: {
    query?: string;
    state?: string;
    teacherId?: number;
    limit?: number;
    offset?: number;
  }) {
    return this.request<{
      rooms: RoomWithTeacher[];
      total: number;
      limit: number;
      offset: number;
      teachers: TeacherOption[];
    }>('GET', `/admin/classrooms${queryString(params)}`);
  }

  getClassroom(id: number) {
    return this.request<{
      room: Classroom;
      teacher?: { id: number; email: string; nickname: string; role: string };
      participants: Participant[];
      attachments: Attachment[];
      audit: AuditEntry[];
    }>('GET', `/admin/classrooms/${id}`);
  }

  endClassroom(id: number) {
    return this.request<{ room: Classroom }>('POST', `/admin/classrooms/${id}/end`, {});
  }

  listAttachments(params: { roomId?: number; status?: string; limit?: number; offset?: number }) {
    return this.request<{ attachments: Attachment[]; total: number; limit: number; offset: number }>(
      'GET',
      `/admin/attachments${queryString(params)}`,
    );
  }

  deleteAttachment(id: number) {
    return this.request<{ deleted: boolean }>('DELETE', `/admin/attachments/${id}`);
  }

  listAudit(params: { roomId?: number; action?: string; limit?: number; offset?: number }) {
    return this.request<{ entries: AuditEntry[]; total: number; limit: number; offset: number }>(
      'GET',
      `/admin/audit${queryString(params)}`,
    );
  }

  getSettings() {
    return this.request<{ settings: Settings }>('GET', '/admin/settings');
  }

  updateSettings(settings: Partial<Settings>) {
    return this.request<{ settings: Settings }>('PUT', '/admin/settings', settings);
  }

  attachmentUrl(roomId: number, attachmentId: number) {
    return `${this.baseUrl}/classrooms/${roomId}/attachments/${attachmentId}/download?token=${encodeURIComponent(this.token || '')}`;
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = {};
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const response = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
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

function queryString(params: Record<string, string | number | undefined>) {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') continue;
    search.set(key, String(value));
  }
  const raw = search.toString();
  return raw ? `?${raw}` : '';
}
