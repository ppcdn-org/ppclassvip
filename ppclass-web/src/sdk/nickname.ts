// Nickname helpers shared by the student join page and the teacher workbench.
//
// Both sides must be able to pick a name for the classroom, and the browser
// should remember the names used before so the next visit can just pick one
// from the list instead of retyping it. Kept separate from the session helpers
// because these are plain preferences, not credentials.

const STUDENT_CURRENT = 'ppclass.nickname';
const STUDENT_LIST = 'ppclass.nicknames';
const TEACHER_CURRENT = 'ppclass.teacher.nickname';
const TEACHER_LIST = 'ppclass.teacher.nicknames';
const MAX_SAVED = 8;

function storage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
}

function readList(key: string): string[] {
  const store = storage();
  if (!store) return [];
  try {
    const parsed = JSON.parse(store.getItem(key) || '[]');
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((item): item is string => typeof item === 'string')
      .map((item) => item.trim())
      .filter(Boolean)
      .slice(0, MAX_SAVED);
  } catch {
    return [];
  }
}

function remember(key: string, name: string) {
  const store = storage();
  if (!store) return;
  const trimmed = name.trim();
  if (!trimmed) return;
  const next = [trimmed, ...readList(key).filter((item) => item !== trimmed)].slice(0, MAX_SAVED);
  store.setItem(key, JSON.stringify(next));
}

function current(storeKey: string, listKey: string): string {
  const store = storage();
  if (!store) return '';
  return (store.getItem(storeKey) || '').trim() || readList(listKey)[0] || '';
}

export function loadStudentNickname(): string {
  return current(STUDENT_CURRENT, STUDENT_LIST);
}

export function listStudentNicknames(): string[] {
  return readList(STUDENT_LIST);
}

export function saveStudentNickname(name: string) {
  const store = storage();
  const trimmed = name.trim();
  if (!store || !trimmed) return;
  store.setItem(STUDENT_CURRENT, trimmed);
  remember(STUDENT_LIST, trimmed);
}

export function loadTeacherNickname(): string {
  return current(TEACHER_CURRENT, TEACHER_LIST);
}

export function listTeacherNicknames(): string[] {
  return readList(TEACHER_LIST);
}

export function saveTeacherNickname(name: string) {
  const store = storage();
  const trimmed = name.trim();
  if (!store || !trimmed) return;
  store.setItem(TEACHER_CURRENT, trimmed);
  remember(TEACHER_LIST, trimmed);
}
