export { PpclassApi, ApiError, API_BASE, websocketUrl, remoteControlUrl } from './api';
export { ClassroomWebSocket } from './ws';

export { ControlSocket } from './control';
export { ClassroomMedia } from './media';
export { ScreenRecorder, isRecordingSupported } from './ppscreenrecorder';
export * from './types';
export {
  loadTeacherToken,
  saveTeacherToken,
  clearTeacherToken,
  loadRoomSession,
  saveRoomSession,
  clearRoomSession,
} from './session';
export {
  loadStudentNickname,
  listStudentNicknames,
  saveStudentNickname,
  loadTeacherNickname,
  listTeacherNicknames,
  saveTeacherNickname,
} from './nickname';
