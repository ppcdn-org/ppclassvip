import type { RoomSession } from './types';

const TEACHER_TOKEN_KEY = 'ppclass.teacherToken';
const ROOM_KEY_PREFIX = 'ppclass.room.';

export function loadTeacherToken() {
  return localStorage.getItem(TEACHER_TOKEN_KEY) || '';
}

export function saveTeacherToken(token: string) {
  localStorage.setItem(TEACHER_TOKEN_KEY, token);
}

export function clearTeacherToken() {
  localStorage.removeItem(TEACHER_TOKEN_KEY);
}

function readRoomSession(storage: Storage, roomId: number): RoomSession | null {
  const raw = storage.getItem(`${ROOM_KEY_PREFIX}${roomId}`);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as RoomSession;
    if (parsed.roomId !== roomId || !parsed.roomToken) return null;
    if (parsed.expiresAt && Date.parse(parsed.expiresAt) <= Date.now()) return null;
    return parsed;
  } catch {
    return null;
  }
}

export function loadRoomSession(roomId: number): RoomSession | null {
  // Students use per-tab sessionStorage so opening an invite in another tab
  // cannot overwrite the teacher's room token on the same origin.
  return readRoomSession(sessionStorage, roomId) ?? readRoomSession(localStorage, roomId);
}

export function saveRoomSession(session: RoomSession) {
  const storage = session.role === 'student' ? sessionStorage : localStorage;
  storage.setItem(`${ROOM_KEY_PREFIX}${session.roomId}`, JSON.stringify(session));
}

export function clearRoomSession(roomId: number) {
  localStorage.removeItem(`${ROOM_KEY_PREFIX}${roomId}`);
  sessionStorage.removeItem(`${ROOM_KEY_PREFIX}${roomId}`);
}
