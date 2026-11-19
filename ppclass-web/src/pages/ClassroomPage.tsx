import { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  ApiError,
  ClassroomMedia,
  ClassroomWebSocket,
  ControlSocket,
  PpclassApi,
  ScreenRecorder,
  clearRoomSession,
  isRecordingSupported,
  loadRoomSession,
  loadTeacherNickname,
  loadTeacherToken,
  saveRoomSession,
  type Attachment,
  type ChatMessage,
  type Classroom,
  type Participant,
  type RemoteDesktopMode,
  type RoomSession,
  type WsEvent,
} from '../sdk';

type RecordingInfo = { recordingId: string; status: string } | null;
type ConsentRequest = { recordingId: string; teacherName: string; expiresAt: string } | null;
type LinkStatus = 'connecting' | 'open' | 'reconnecting' | 'closed';
type RemoteDesktopRequest = { sessionId: string; teacherName: string; mode: RemoteDesktopMode; expiresAt: string } | null;
type RemoteDesktopState = { sessionId: string; mode: RemoteDesktopMode; status: string; expiresAt?: string; controllerToken?: string } | null;

function newClientMsgId() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) return crypto.randomUUID();
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function isClosedState(state: string) {
  return state === 'ended' || state === 'failed' || state === 'expired';
}

function participantLabel(participants: Participant[], id: number) {
  return participants.find((item) => item.id === id)?.displayName || `参与者 ${id}`;
}

export default function ClassroomPage() {
  const { roomId: roomIdParam } = useParams();
  const roomId = Number(roomIdParam);
  const navigate = useNavigate();

  const [session, setSession] = useState<RoomSession | null>(null);
  const [room, setRoom] = useState<Classroom | null>(null);
  const [participants, setParticipants] = useState<Participant[]>([]);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [streams, setStreams] = useState<Record<string, MediaStream | undefined>>({});
  const [screenOwner, setScreenOwner] = useState<number | null>(null);
  const [recording, setRecording] = useState<RecordingInfo>(null);
  const [consentRequest, setConsentRequest] = useState<ConsentRequest>(null);
  const [recorderState, setRecorderState] = useState('idle');
  const [wsStatus, setWsStatus] = useState<LinkStatus>('connecting');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [chatText, setChatText] = useState('');
  const [inviteLink, setInviteLink] = useState('');
  const [uploading, setUploading] = useState(false);
  const [pendingAttachment, setPendingAttachment] = useState<Attachment | null>(null);
  const [micOn, setMicOn] = useState(true);
  const [camOn, setCamOn] = useState(true);
  const [screenRequested, setScreenRequested] = useState(false);
  const [remote, setRemote] = useState<RemoteDesktopState>(null);
  const [remoteRequest, setRemoteRequest] = useState<RemoteDesktopRequest>(null);
  const [remoteModalOpen, setRemoteModalOpen] = useState(false);
  const [remoteError, setRemoteError] = useState('');
  const [controlStatus, setControlStatus] = useState('closed');
  const [keyboardEnabled, setKeyboardEnabled] = useState(false);
  const [agentCode, setAgentCode] = useState<{ code: string; expiresAt: string } | null>(null);
  const [agentCodeOpen, setAgentCodeOpen] = useState(false);

  const sessionRef = useRef<RoomSession | null>(null);
  const roomRef = useRef<Classroom | null>(null);
  const participantsRef = useRef<Participant[]>([]);
  const apiRef = useRef<PpclassApi | null>(null);
  const mediaRef = useRef<ClassroomMedia | null>(null);
  const wsRef = useRef<ClassroomWebSocket | null>(null);
  const recorderRef = useRef<ScreenRecorder | null>(null);
  const recordingRef = useRef<RecordingInfo>(null);
  const messagesRef = useRef<ChatMessage[]>([]);
  const screenRequestedRef = useRef(false);
  const startingCaptureRef = useRef(false);
  const cameraPublishedRef = useRef(false);
  const screenSharingRef = useRef(false);
  const wsEverOpenRef = useRef(false);
  const subscribingRef = useRef(new Set<string>());
  const mainVideoRef = useRef<HTMLVideoElement | null>(null);
  const localVideoRef = useRef<HTMLVideoElement | null>(null);
  const remoteAudioRef = useRef<HTMLAudioElement | null>(null);
  const controlRef = useRef<ControlSocket | null>(null);
  const controlLayerRef = useRef<HTMLDivElement | null>(null);
  const lastMoveSentRef = useRef(0);
  const remoteRef = useRef<RemoteDesktopState>(null);

  const role = session?.role || 'student';
  const ownCameraLeg = role === 'teacher' ? 'teacher-cam' : 'student-cam';
  const peerCameraLeg = role === 'teacher' ? 'student-cam' : 'teacher-cam';
  const ownScreenLeg = role === 'teacher' ? 'teacher-screen' : 'student-screen';
  const peerScreenLeg = room?.screenLeg || (role === 'teacher' ? 'student-screen' : 'teacher-screen');
  const localCameraStream = streams[ownCameraLeg];
  const mainStream = screenOwner
    ? screenOwner === session?.participantId
      ? streams[ownScreenLeg] ?? streams[peerCameraLeg]
      : streams[peerScreenLeg] ?? streams[peerCameraLeg]
    : streams[peerCameraLeg];
  const ended = room ? isClosedState(room.state) : false;
  const recordingInProgress = Boolean(
    recording && ['pending_consent', 'recording', 'finalizing'].includes(recording.status),
  );
  const canSendChat = Boolean(room?.chatEnabled) || role === 'teacher';

  const activeStudent = participants.find((item) => item.role === 'student' && !item.leftAt);

  useEffect(() => {
    sessionRef.current = session;
  }, [session]);

  useEffect(() => {
    roomRef.current = room;
  }, [room]);

  useEffect(() => {
    remoteRef.current = remote;
  }, [remote]);

  useEffect(() => {
    participantsRef.current = participants;
  }, [participants]);

  useEffect(() => {
    if (!Number.isFinite(roomId) || roomId <= 0) {
      setError('无效的课堂 ID');
      setLoading(false);
      return;
    }
    let cancelled = false;
    void (async () => {
      const existing = loadRoomSession(roomId);
      if (existing) {
        if (cancelled) return;
        sessionRef.current = existing;
        setSession(existing);
        setLoading(false);
        return;
      }
      const teacherToken = loadTeacherToken();
      if (!teacherToken) {
        if (!cancelled) {
          setError('没有课堂凭证。教师请先登录，学生请使用邀请链接。');
          setLoading(false);
        }
        return;
      }
      try {
        const api = new PpclassApi();
        api.setToken(teacherToken);
        const joined = await api.teacherJoin(roomId, loadTeacherNickname());
        const next: RoomSession = {
          roomId,
          roomToken: joined.roomToken,
          participantId: joined.participant.id,
          role: 'teacher',
          displayName: joined.participant.displayName,
          expiresAt: joined.expiresAt,
        };
        saveRoomSession(next);
        if (!cancelled) {
          sessionRef.current = next;
          setSession(next);
          setLoading(false);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : '无法进入课堂');
          setLoading(false);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [roomId]);

  useEffect(() => {
    if (!session) return;
    let disposed = false;
    const api = new PpclassApi();
    api.setToken(session.roomToken);
    apiRef.current = api;
    const media = new ClassroomMedia(
      (leg, stream) => {
        setStreams((current) => {
          const next = { ...current };
          if (stream) next[leg] = stream;
          else delete next[leg];
          return next;
        });
      },
      (message) => setError(message),
    );
    mediaRef.current = media;
    const ws = new ClassroomWebSocket({
      url: api.wsUrl(roomId),
      token: session.roomToken,
      onEvent: (event) => handleEvent(event),
      onStatus: (status) => {
        setWsStatus(status);
        if (status === 'open') {
          if (wsEverOpenRef.current) {
            void recoverMedia();
            void catchUpMessages();
          } else {
            wsEverOpenRef.current = true;
          }
        }
      },
      onClose: (_code, reason) => {
        if ((reason === 'room_ended' || reason === 'room_deleted') && session.role === 'student') {
          clearRoomSession(roomId);
          navigate('/');
        }
      },
    });
    wsRef.current = ws;

    void (async () => {
      try {
        const state = await api.getClassroom(roomId);
        if (disposed) return;
        applyRoom(state.room);
        setParticipants(state.participants);
        const history = await api.listMessages(roomId);
        if (disposed) return;
        setMessages(history.messages);
        messagesRef.current = history.messages;
      } catch (err) {
        if (!disposed) setError(err instanceof Error ? err.message : '加载课堂失败');
      } finally {
        if (!disposed) {
          setLoading(false);
          ws.connect();
        }
      }
    })();

    return () => {
      disposed = true;
      ws.close();
      media.stopAll();
      recorderRef.current?.abort();
      recorderRef.current = null;
      apiRef.current = null;
      mediaRef.current = null;
      wsRef.current = null;
      cameraPublishedRef.current = false;
      screenSharingRef.current = false;
      wsEverOpenRef.current = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session, roomId]);

  useEffect(() => {
    if (mainVideoRef.current) mainVideoRef.current.srcObject = mainStream || null;
  }, [mainStream]);

  useEffect(() => {
    if (localVideoRef.current) localVideoRef.current.srcObject = localCameraStream || null;
  }, [localCameraStream]);

  useEffect(() => {
    if (remoteAudioRef.current) remoteAudioRef.current.srcObject = streams[peerCameraLeg] || null;
  }, [streams, peerCameraLeg]);

  useEffect(() => {
    const list = document.querySelector('.chat-list');
    if (list) list.scrollTop = list.scrollHeight;
  }, [messages]);

  function applyRoom(next: Classroom) {
    roomRef.current = next;
    setRoom(next);
    setScreenOwner(next.screenOwnerParticipantId ?? null);
  }

  function addMessage(message: ChatMessage) {
    setMessages((current) => {
      if (current.some((item) => item.id === message.id)) return current;
      const next = [...current, message];
      messagesRef.current = next;
      return next;
    });
  }

  async function refreshParticipants() {
    const api = apiRef.current;
    if (!api) return;
    try {
      const state = await api.getClassroom(roomId);
      applyRoom(state.room);
      setParticipants(state.participants);
    } catch {
      // Reconnect/sync errors are transient; the WS will keep them fresh.
    }
  }

  async function catchUpMessages() {
    const api = apiRef.current;
    if (!api) return;
    const last = messagesRef.current[messagesRef.current.length - 1]?.id ?? 0;
    try {
      const result = await api.listMessages(roomId, last);
      for (const message of result.messages) addMessage(message);
    } catch {
      // The next reconnect will retry.
    }
  }

  function handleEvent(event: WsEvent) {
    const currentSession = sessionRef.current;
    if (!currentSession) return;
    const data = (event.data || {}) as Record<string, unknown>;
    switch (event.type) {
      case 'room.state': {
        if (data.room) applyRoom(data.room as Classroom);
        if (Array.isArray(data.participants)) setParticipants(data.participants as Participant[]);
        break;
      }
      case 'room.joined':
      case 'presence.joined':
      case 'room.left':
      case 'presence.left':
        void refreshParticipants();
        break;
      case 'chat.message':
        addMessage(data as unknown as ChatMessage);
        break;
      case 'chat.ack': {
        const message = data.message as ChatMessage | undefined;
        if (message) addMessage(message);
        break;
      }
      case 'screen.state': {
        const active = Boolean(data.active);
        const owner = Number(data.owner || 0);
        setScreenOwner(active && owner ? owner : null);
        if (!active && owner === currentSession.participantId) {
          screenSharingRef.current = false;
          mediaRef.current?.stopScreen();
          setStreams((current) => {
            const next = { ...current };
            delete next[ownScreenLeg];
            return next;
          });
        }
        if (active && owner === currentSession.participantId) screenSharingRef.current = true;
        if (active && owner && owner !== currentSession.participantId) {
          void subscribeLeg(String(data.leg || peerScreenLeg));
        } else if (!active) {
          mediaRef.current?.stopSubscribe(peerScreenLeg);
          // Screen sharing can outlive/restart the camera subscription; make
          // sure the camera leg is live again so the main video falls back to
          // the peer camera (and its audio) instead of going blank.
          void subscribeLeg(peerCameraLeg);
        }
        break;
      }
      case 'peer.publishing': {
        const leg = String(data.leg || '');
        const publisherId = Number(data.participantId || 0);
        if (!leg || publisherId === currentSession.participantId) break;
        if (leg === peerCameraLeg || leg === peerScreenLeg) void subscribeLeg(leg);
        break;
      }
      case 'peer.unpublished': {
        const leg = String(data.leg || '');
        if (leg) mediaRef.current?.stopSubscribe(leg);
        break;
      }
      case 'recording.request':
        setConsentRequest({
          recordingId: String(data.recordingId || ''),
          teacherName: String(data.teacherName || '老师'),
          expiresAt: String(data.expiresAt || ''),
        });
        break;
      case 'recording.consented': {
        if (currentSession.role !== 'teacher') break;
        const id = String(data.recordingId || '');
        void startCaptureRecording(id);
        break;
      }
      case 'recording.declined': {
        setRecording(null);
        recordingRef.current = null;
        setRecorderState('idle');
        setNotice('学生未同意录屏，录制未开始。');
        break;
      }
      case 'recording.stop.requested': {
        if (currentSession.role === 'teacher') void stopRecording();
        else setNotice('已请求老师停止录屏。');
        break;
      }
      case 'recording.state': {
        const id = String(data.recordingId || '');
        const state = String(data.state || '');
        if (!state || !id) break;
        if (['completed', 'failed', 'declined', 'purged'].includes(state)) {
          setRecording(null);
        } else {
          setRecording({ recordingId: id, status: state });
        }
        break;
      }
      case 'control.mute': {
        const participantId = Number(data.participantId || 0);
        const muted = Boolean(data.muted);
        setParticipants((current) =>
          current.map((item) => (item.id === participantId ? { ...item, muted } : item)),
        );
        if (participantId === currentSession.participantId) {
          mediaRef.current?.setMicEnabled(!muted);
          setMicOn(!muted);
          setNotice(muted ? '老师已静音你的麦克风。' : '老师已取消静音。');
        }
        break;
      }
      case 'control.screen.permission': {
        const granted = Boolean(data.granted);
        setRoom((current) => (current ? { ...current, studentScreenAllowed: granted } : current));
        if (currentSession.role === 'student') {
          if (granted && screenRequestedRef.current) {
            screenRequestedRef.current = false;
            setScreenRequested(false);
            void startScreenShare();
          } else if (!granted && screenRequestedRef.current) {
            screenRequestedRef.current = false;
            setScreenRequested(false);
            setNotice('老师暂未允许共享屏幕。');
          }
        }
        break;
      }
      case 'control.chat': {
        const enabled = Boolean(data.enabled);
        setRoom((current) => (current ? { ...current, chatEnabled: enabled } : current));
        setNotice(enabled ? '聊天已开启。' : '老师已关闭学生聊天。');
        break;
      }
      case 'remote_desktop.request':
        setRemoteRequest({
          sessionId: String(data.sessionId || ''),
          teacherName: String(data.teacherName || '老师'),
          mode: (String(data.mode || 'view_only') as RemoteDesktopMode),
          expiresAt: String(data.expiresAt || ''),
        });
        setRemoteError('');
        break;
      case 'remote_desktop.consented': {
        if (currentSession.role !== 'teacher') break;
        const mode = String(data.mode || 'view_only') as RemoteDesktopMode;
        const sessionId = String(data.sessionId || '');
        const token = String(data.controllerToken || '');
        setRemote({ sessionId, mode, status: 'active', expiresAt: String(data.expiresAt || ''), controllerToken: token });
        setNotice(mode === 'control' ? '学生已同意远程控制。' : '学生已同意远程查看。');
        if (mode === 'control' && token) startRemoteControl(sessionId, token);
        break;
      }
      case 'remote_desktop.declined':
        setRemote(null);
        setNotice('学生拒绝了远程桌面请求。');
        break;
      case 'remote_desktop.state': {
        const sessionId = String(data.sessionId || '');
        setRemote((current) => {
          if (!current || current.sessionId !== sessionId) return current;
          return { ...current, status: String(data.status || current.status), mode: (String(data.mode || current.mode) as RemoteDesktopMode) };
        });
        break;
      }
      case 'remote_desktop.stopped':
        setNotice('远程桌面已结束。');
        cleanupRemoteLocal();
        break;
      case 'room.ended':
        cleanupRemoteLocal();
        mediaRef.current?.stopAll();
        if (currentSession.role === 'student') {
          clearRoomSession(roomId);
          navigate('/');
          break;
        }
        if (roomRef.current) applyRoom({ ...roomRef.current, state: 'ended' });
        setNotice('课堂已结束。');
        break;
      case 'error': {
        const message = String(data.message || '课堂操作失败');
        setError(message);
        break;
      }
      default:
        break;
    }
  }

  async function subscribeLeg(leg: string) {
    const api = apiRef.current;
    const media = mediaRef.current;
    if (!api || !media) return;
    if (subscribingRef.current.has(leg)) return;
    subscribingRef.current.add(leg);
    try {
      // Give the Origin/Edge a moment to register the freshly published
      // pipeline before asking ppcenter for a play decision. Screen legs take
      // longer because publishing starts after the screen-share API call.
      const screenLeg = leg.endsWith('-ts') || leg.endsWith('-ss');
      const initialDelay = screenLeg ? 1800 : 600;
      const postDecisionDelay = screenLeg ? 1800 : 800;
      await new Promise((resolve) => window.setTimeout(resolve, initialDelay));
      for (let attempt = 0; attempt < 6; attempt += 1) {
        try {
          const result = await api.refreshMedia(roomId, leg, 'subscribe');
          await new Promise((resolve) => window.setTimeout(resolve, postDecisionDelay));
          media.subscribe(leg, result.credential);
          return;
        } catch (err) {
          const transient =
            err instanceof ApiError &&
            (err.code === 'no_edge_available' || err.status === 404 || err.status === 503);
          if (!transient) throw err;
          if (attempt === 5) return;
          await new Promise((resolve) => window.setTimeout(resolve, 1000 * (attempt + 1)));
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '订阅媒体失败');
    } finally {
      subscribingRef.current.delete(leg);
    }
  }

  async function recoverMedia() {
    const api = apiRef.current;
    const media = mediaRef.current;
    if (!api || !media || !sessionRef.current) return;
    if (cameraPublishedRef.current) {
      try {
        const result = await api.refreshMedia(roomId, ownCameraLeg, 'publish');
        await media.republishCamera(result.credential, []);
        wsRef.current?.send('media.ready', { leg: ownCameraLeg });
      } catch {
        // Keep the existing media; the next manual action can retry.
      }
    }
    if (screenSharingRef.current) {
      try {
        const result = await api.refreshMedia(roomId, ownScreenLeg, 'publish');
        await media.republishScreen(result.credential, []);
        wsRef.current?.send('media.ready', { leg: ownScreenLeg });
      } catch {
        // Screen recovery is best-effort.
      }
    }
    for (const leg of media.getSubscribedLegs()) {
      await subscribeLeg(leg);
    }
  }

  async function startCamera() {
    const api = apiRef.current;
    const media = mediaRef.current;
    if (!api || !media) return;
    try {
      const result = await api.refreshMedia(roomId, ownCameraLeg, 'publish');
      const stream = await media.publishCamera(result.credential, []);
      setStreams((current) => ({ ...current, [ownCameraLeg]: stream }));
      cameraPublishedRef.current = true;
      wsRef.current?.send('media.ready', { leg: ownCameraLeg });
      setMicOn(true);
      setCamOn(true);
    } catch (err) {
      media.stopCamera();
      setError(err instanceof Error ? err.message : '开启摄像头失败');
    }
  }

  function toggleMic() {
    const next = !micOn;
    mediaRef.current?.setMicEnabled(next);
    setMicOn(next);
  }

  function toggleCamera() {
    const next = !camOn;
    mediaRef.current?.setCameraEnabled(next);
    setCamOn(next);
  }

  async function requestOrStartScreen() {
    if (ended) return;
    if (role === 'student' && !room?.studentScreenAllowed) {
      screenRequestedRef.current = true;
      setScreenRequested(true);
      wsRef.current?.send('screen.start.request', {});
      setNotice('已向老师申请共享屏幕，等待老师允许。');
      return;
    }
    await startScreenShare();
  }

  async function startScreenShare() {
    const api = apiRef.current;
    const media = mediaRef.current;
    if (!api || !media) return;
    try {
      const started = await api.startScreen(roomId);
      const stream = await media.publishScreen(started.publish, []);
      setStreams((current) => ({ ...current, [started.leg]: stream }));
      screenSharingRef.current = true;
      wsRef.current?.send('media.ready', { leg: started.leg });
      setScreenOwner(session?.participantId ?? null);
    } catch (err) {
      media.stopScreen();
      setError(err instanceof Error ? err.message : '共享屏幕失败');
    }
  }

  async function stopScreenShare() {
    const api = apiRef.current;
    const media = mediaRef.current;
    if (!api || !media) return;
    if (recorderRef.current) await stopRecording();
    try {
      await api.stopScreen(roomId);
    } catch (err) {
      setError(err instanceof Error ? err.message : '停止共享失败');
    }
    media.stopScreen();
    setStreams((current) => {
      const next = { ...current };
      delete next[ownScreenLeg];
      return next;
    });
    screenSharingRef.current = false;
    setScreenOwner(null);
  }

  async function startRecording() {
    const api = apiRef.current;
    const media = mediaRef.current;
    if (!api || !media) return;
    if (!media.getLocalCameraStream()) {
      setError('请先开启摄像头/麦克风，再开始录屏。');
      return;
    }
    if (!media.getRemoteAudioTrack()) {
      setError('还没有收到学生的声音，等学生加入并推流后再录屏。');
      return;
    }
    try {
      const started = await api.startRecording(roomId, true);
      const info = { recordingId: started.recordingId, status: started.status };
      recordingRef.current = info;
      setRecording(info);
      setRecorderState('waiting_consent');
      setNotice('已请求学生同意录屏，等待确认…');
    } catch (err) {
      setError(err instanceof Error ? err.message : '发起录屏失败');
    }
  }

  async function startCaptureRecording(recordingId: string) {
    const api = apiRef.current;
    const media = mediaRef.current;
    const currentSession = sessionRef.current;
    if (!api || !media || !currentSession || startingCaptureRef.current) return;
    if (!recordingId) return;
    startingCaptureRef.current = true;
    try {
      let screen = media.getScreenStream();
      if (!screen) {
        const started = await api.startScreen(roomId);
        const published = await media.publishScreen(started.publish, []);
        screen = published;
        setStreams((current) => ({ ...current, [started.leg]: published }));
        screenSharingRef.current = true;
        wsRef.current?.send('media.ready', { leg: started.leg });
        setScreenOwner(currentSession.participantId);
      }
      const mic = media.getLocalCameraStream();
      if (!mic) throw new Error('请先开启麦克风');
      const recorder = new ScreenRecorder({
        onState: (state, detail) => {
          setRecorderState(state);
          if (detail) setError(detail);
        },
        onError: (err) => setError(err.message),
      });
      recorderRef.current = recorder;
      await recorder.start({
        screenStream: screen,
        micStream: mic,
        remoteAudioTrack: media.getRemoteAudioTrack(),
      });
      const info = { recordingId, status: 'recording' };
      recordingRef.current = info;
      setRecording(info);
      setNotice('录制中：录屏会保存在本机，请勿关闭页面，建议戴耳机。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '开始录屏失败');
      await api.stopRecording(roomId, recordingId).catch(() => undefined);
      recordingRef.current = null;
      setRecording(null);
      setRecorderState('idle');
    } finally {
      startingCaptureRef.current = false;
    }
  }

  async function stopRecording() {
    const api = apiRef.current;
    const recorder = recorderRef.current;
    const current = recordingRef.current;
    if (!api || !recorder || !current) return;
    setRecorderState('stopping');
    try {
      const blob = await recorder.stop();
      saveRecordingFile(blob, current.recordingId);
      await api.stopRecording(roomId, current.recordingId);
      setRecording(null);
      setNotice('录屏已保存到本地。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '结束录屏失败');
      await api.stopRecording(roomId, current.recordingId).catch(() => undefined);
      setRecording(null);
    } finally {
      recorderRef.current = null;
      recordingRef.current = null;
      setRecorderState('idle');
    }
  }

  function saveRecordingFile(blob: Blob, recordingId: string) {
    if (blob.size === 0) {
      setError('录屏内容为空，未生成文件。');
      return;
    }
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `ppclass-${roomId}-${recordingId}.webm`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
  }

  async function acceptConsent() {
    const api = apiRef.current;
    if (!api || !consentRequest) return;
    try {
      await api.consentRecording(roomId, consentRequest.recordingId);
      setRecording({ recordingId: consentRequest.recordingId, status: 'recording' });
      setConsentRequest(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '同意录屏失败');
    }
  }

  async function declineConsent() {
    const api = apiRef.current;
    if (!api || !consentRequest) return;
    try {
      await api.declineRecording(roomId, consentRequest.recordingId);
    } catch (err) {
      setError(err instanceof Error ? err.message : '拒绝录屏失败');
    } finally {
      setConsentRequest(null);
    }
  }

  function requestStopRecording() {
    wsRef.current?.send('recording.stop.request', {});
    setNotice('已请求老师停止录屏。');
  }

  function sendText(event?: React.FormEvent) {
    event?.preventDefault();
    if (!canSendChat || ended) return;
    const body = chatText.trim();
    const attachment = pendingAttachment;
    if ((!body && !attachment) || !wsRef.current) return;
    if (attachment) {
      wsRef.current.send('chat.send', {
        clientMsgId: newClientMsgId(),
        messageType: attachment.kind === 'image' ? 'image' : 'file',
        body: body || attachment.originalName,
        attachmentId: attachment.id,
      });
    } else {
      wsRef.current.send('chat.send', {
        clientMsgId: newClientMsgId(),
        messageType: 'text',
        body,
      });
    }
    setChatText('');
    setPendingAttachment(null);
  }

  // Enter sends, Shift+Enter inserts a newline. isComposing guards the IME
  // candidate window used for Chinese/Japanese input, where Enter confirms a
  // candidate and must not submit the message.
  function onChatKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== 'Enter' || event.shiftKey) return;
    if (event.nativeEvent.isComposing) return;
    event.preventDefault();
    sendText();
  }

  async function copyInviteLink() {
    const teacherToken = loadTeacherToken();
    if (!teacherToken) {
      setError('请先登录教师账号，再生成邀请链接。');
      return;
    }
    const teacherApi = new PpclassApi();
    teacherApi.setToken(teacherToken);
    try {
      const result = await teacherApi.createInvite(roomId);
      const link = `${window.location.origin}/join?invite=${encodeURIComponent(result.inviteToken)}`;
      setInviteLink(link);
      let copied = false;
      try {
        await navigator.clipboard?.writeText(link);
        copied = Boolean(navigator.clipboard);
      } catch {
        copied = false;
      }
      setNotice(copied ? '邀请链接已复制，发给学生即可加入。' : '已生成邀请链接，请手动复制。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '生成邀请链接失败');
    }
  }

  async function uploadFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = '';
    const api = apiRef.current;
    if (!file || !api) return;
    setUploading(true);
    setError('');
    try {
      const uploaded = await api.uploadAttachment(roomId, file);
      setPendingAttachment(uploaded.attachment);
    } catch (err) {
      setError(err instanceof Error ? err.message : '上传文件失败');
    } finally {
      setUploading(false);
    }
  }

  async function muteStudent() {
    const api = apiRef.current;
    if (!api || !activeStudent) return;
    try {
      await api.control(roomId, 'mute', {
        participantId: activeStudent.id,
        muted: !activeStudent.muted,
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : '静音操作失败');
    }
  }

  async function toggleStudentScreen() {
    const api = apiRef.current;
    if (!api) return;
    try {
      await api.control(roomId, 'screen.permission', { granted: !room?.studentScreenAllowed });
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新屏幕权限失败');
    }
  }

  async function toggleChat() {
    const api = apiRef.current;
    if (!api) return;
    try {
      await api.control(roomId, 'chat', { enabled: !room?.chatEnabled });
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新聊天权限失败');
    }
  }

  async function leaveClassroom() {
    const api = apiRef.current;
    if (api) await api.leaveClassroom(roomId).catch(() => undefined);
    clearRoomSession(roomId);
    navigate('/');
  }

  async function endClassroom() {
    if (!window.confirm('确定结束课堂吗？结束前请先停止录屏。')) return;
    const api = apiRef.current;
    if (!api) return;
    if (recorderRef.current) await stopRecording();
    try {
      await api.endClassroom(roomId);
      clearRoomSession(roomId);
      navigate('/');
    } catch (err) {
      setError(err instanceof Error ? err.message : '结束课堂失败');
    }
  }

  function cleanupRemoteLocal() {
    controlRef.current?.close();
    controlRef.current = null;
    setControlStatus('closed');
    setKeyboardEnabled(false);
    setRemote(null);
    setRemoteRequest(null);
    setRemoteError('');
    setAgentCode(null);
    setAgentCodeOpen(false);
    if (screenSharingRef.current) {
      mediaRef.current?.stopScreen();
      setStreams((current) => {
        const next = { ...current };
        delete next[ownScreenLeg];
        return next;
      });
      screenSharingRef.current = false;
      setScreenOwner(null);
      apiRef.current?.stopScreen(roomId).catch(() => undefined);
    }
  }

  function startRemoteControl(sessionId: string, token: string) {
    const api = apiRef.current;
    if (!api) return;
    controlRef.current?.close();
    const socket = new ControlSocket(
      api.remoteControlUrl(roomId, sessionId),
      token,
      (status) => setControlStatus(status),
      (message) => setRemoteError(message),
    );
    controlRef.current = socket;
    socket.connect();
  }

  async function requestRemoteDesktop(mode: RemoteDesktopMode) {
    const api = apiRef.current;
    if (!api) return;
    setRemoteError('');
    try {
      const result = await api.requestRemoteDesktop(roomId, mode);
      setRemote({
        sessionId: result.session.sessionId,
        mode,
        status: 'pending_consent',
        expiresAt: result.expiresAt,
      });
      setRemoteModalOpen(false);
      setNotice('已向学生发起远程桌面请求，等待同意。');
    } catch (err) {
      setRemoteError(err instanceof Error ? err.message : '发起远程桌面失败');
    }
  }

  async function acceptRemoteRequest(mode: RemoteDesktopMode) {
    const api = apiRef.current;
    const request = remoteRequest;
    if (!api || !request) return;
    setRemoteError('');
    try {
      const result = await api.consentRemoteDesktop(roomId, request.sessionId, mode);
      setRemote({
        sessionId: request.sessionId,
        mode,
        status: 'active',
        expiresAt: result.expiresAt,
      });
      setRemoteRequest(null);
      if (result.agentCode) {
        setAgentCode({ code: result.agentCode, expiresAt: result.agentCodeExpiresAt });
        setAgentCodeOpen(true);
      }
      setNotice(mode === 'control' ? '已允许老师控制你的桌面。' : '已允许老师查看你的桌面。');
    } catch (err) {
      setRemoteError(err instanceof Error ? err.message : '同意远程桌面失败');
    }
  }
  async function declineRemoteRequest() {
    const api = apiRef.current;
    const request = remoteRequest;
    if (!api || !request) return;
    try {
      await api.declineRemoteDesktop(roomId, request.sessionId);
    } catch {
      // Nothing else to do; the request also expires server-side.
    }
    setRemoteRequest(null);
  }

  async function stopRemoteDesktop() {
    const api = apiRef.current;
    const current = remoteRef.current;
    if (!api || !current) return;
    try {
      await api.stopRemoteDesktop(roomId, current.sessionId);
    } catch {
      // The session may already be stopped.
    }
    cleanupRemoteLocal();
  }

  function sendRemoteMove(event: React.MouseEvent<HTMLDivElement>) {
    const current = remoteRef.current;
    if (!current || current.mode !== 'control' || controlStatus !== 'open') return;
    const now = performance.now();
    if (now - lastMoveSentRef.current < 16) return;
    const rect = controlLayerRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) return;
    const x = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
    const y = Math.min(1, Math.max(0, (event.clientY - rect.top) / rect.height));
    lastMoveSentRef.current = now;
    controlRef.current?.send({ type: 'mouse.move', x, y, ts: Date.now() });
  }

  function sendRemoteButton(event: React.MouseEvent<HTMLDivElement>, action: 'down' | 'up') {
    event.preventDefault();
    const button = event.button === 2 ? 'right' : event.button === 1 ? 'middle' : 'left';
    controlRef.current?.send({ type: 'mouse.button', button, action });
  }

  function sendRemoteWheel(event: React.WheelEvent<HTMLDivElement>) {
    event.preventDefault();
    controlRef.current?.send({ type: 'mouse.wheel', dx: event.deltaX, dy: event.deltaY });
  }

  function sendRemoteKey(event: React.KeyboardEvent<HTMLDivElement>, action: 'down' | 'up') {
    if (action === 'down' && event.code === 'Escape') {
      setKeyboardEnabled(false);
      event.currentTarget.blur();
      event.preventDefault();
      return;
    }
    if (!keyboardEnabled && action === 'down') return;
    event.preventDefault();
    const modifiers = [
      event.ctrlKey ? 'ctrl' : '',
      event.shiftKey ? 'shift' : '',
      event.altKey ? 'alt' : '',
      event.metaKey ? 'meta' : '',
    ].filter(Boolean);
    controlRef.current?.send({ type: 'key', code: event.code, action, modifiers });
  }

  const chatMessages = useMemo(
    () => messages.filter((message) => message.type !== 'system'),
    [messages],
  );

  if (loading) {
    return <div className="page narrow muted">正在进入课堂…</div>;
  }

  if (!session) {
    return (
      <div className="page narrow">
        <div className="card stack">
          <h1 style={{ margin: 0 }}>无法进入课堂</h1>
          <div className="error">{error || '缺少课堂凭证。'}</div>
          <Link to="/">返回首页</Link>
        </div>
      </div>
    );
  }

  return (
    <div className="classroom">
      <div className="stage">
        <header className="topbar card">
          <div>
            <div className="row">
              <strong>{room?.title || '1v1 课堂'}</strong>
              <span className="badge">{role === 'teacher' ? '教师' : '学生'}</span>
              {room && <span className="badge">{room.state}</span>}
              {recordingInProgress && <span className="badge recording">录屏中</span>}
              {screenOwner && <span className="badge warn">屏幕共享中</span>}
            </div>
            <div className="muted small row" style={{ marginTop: '0.35rem' }}>
              <span className={`status-dot ${wsStatus}`} />
              <span>
                {wsStatus === 'open'
                  ? '已连接'
                  : wsStatus === 'reconnecting'
                    ? '正在重连'
                    : wsStatus === 'connecting'
                      ? '连接中'
                      : '已断开'}
              </span>
              <span>· 房间 {room?.roomCode || roomId}</span>
            </div>
          </div>
          <div className="row">
            {role === 'student' && (
              <a
                className="button-link small"
                href="/downloads/ppclass-agent.exe"
                target="_blank"
                rel="noreferrer"
              >
                远程协助 Agent
              </a>
            )}
            {role === 'teacher' && !ended && (
              <button onClick={() => void copyInviteLink()}>复制邀请链接</button>
            )}
            <button className="danger" onClick={() => void leaveClassroom()}>
              离开
            </button>
            {role === 'teacher' && (
              <button className="danger" onClick={() => void endClassroom()}>
                结束课堂
              </button>
            )}
          </div>
        </header>

        {inviteLink && (
          <div className="invite-box">
            <div className="small muted">学生邀请链接（一次性，发给学生即可加入）</div>
            <input
              readOnly
              value={inviteLink}
              onFocus={(event) => event.currentTarget.select()}
            />
            <div className="row">
              <button
                onClick={() => {
                  void navigator.clipboard?.writeText(inviteLink);
                  setNotice('邀请链接已复制。');
                }}
              >
                复制链接
              </button>
              <button onClick={() => setInviteLink('')}>收起</button>
            </div>
          </div>
        )}

        {error && <div className="error">{error}</div>}
        {notice && <div className="notice">{notice}</div>}
        {remoteError && <div className="error">{remoteError}</div>}

        <div className="videos">
          {remote && remote.status === 'active' && remote.mode === 'control' && (
            <div
              ref={controlLayerRef}
              className="remote-control-layer"
              tabIndex={0}
              onMouseMove={sendRemoteMove}
              onMouseDown={(event) => sendRemoteButton(event, 'down')}
              onMouseUp={(event) => sendRemoteButton(event, 'up')}
              onContextMenu={(event) => event.preventDefault()}
              onWheel={sendRemoteWheel}
              onKeyDown={(event) => sendRemoteKey(event, 'down')}
              onKeyUp={(event) => sendRemoteKey(event, 'up')}
              onClick={(event) => {
                event.currentTarget.focus();
                setKeyboardEnabled(true);
              }}
            >
              <div className="remote-control-hint">
                {keyboardEnabled ? '键盘已捕获 · Esc 释放' : '点击画面启用键盘 · 鼠标已可控制'}
                {controlStatus !== 'open' ? ' · 控制通道连接中…' : ''}
              </div>
            </div>
          )}
          <video ref={mainVideoRef} className="main-video" autoPlay playsInline muted />
          <audio ref={remoteAudioRef} autoPlay hidden />
          {!mainStream && (
            <div className="video-empty">
              {ended ? '课堂已结束' : role === 'teacher' ? '等待学生画面…' : '等待老师画面…'}
            </div>
          )}
          {localCameraStream && (
            <video ref={localVideoRef} className="self-preview" autoPlay playsInline muted />
          )}
        </div>

        <div className="toolbar card">
          {!localCameraStream ? (
            <button className="primary" onClick={() => void startCamera()} disabled={ended}>
              开启摄像头/麦克风
            </button>
          ) : (
            <>
              <button onClick={toggleMic} disabled={ended}>
                {micOn ? '关闭麦克风' : '开启麦克风'}
              </button>
              <button onClick={toggleCamera} disabled={ended}>
                {camOn ? '关闭摄像头' : '开启摄像头'}
              </button>
            </>
          )}

          {screenOwner === session.participantId ? (
            <button onClick={() => void stopScreenShare()} disabled={ended}>
              停止共享屏幕
            </button>
          ) : (
            <button onClick={() => void requestOrStartScreen()} disabled={ended || screenRequested}>
              {screenRequested ? '等待老师允许…' : '共享屏幕'}
            </button>
          )}

          {role === 'teacher' && (
            <>
              <button
                className="primary"
                onClick={() => void startRecording()}
                disabled={ended || recordingInProgress || Boolean(remote) || !isRecordingSupported()}
                title={isRecordingSupported() ? '' : '当前浏览器不支持 WebM 录屏'}
              >
                开始录屏
              </button>
              <button
                onClick={() => setRemoteModalOpen(true)}
                disabled={ended || recordingInProgress || Boolean(remote)}
              >
                远程桌面
              </button>
              <button onClick={() => void stopRecording()} disabled={!recorderRef.current}>
                停止录屏
              </button>
              <button onClick={() => void muteStudent()} disabled={!activeStudent}>
                {activeStudent?.muted ? '取消静音学生' : '静音学生'}
              </button>
              <button onClick={() => void toggleStudentScreen()}>
                {room?.studentScreenAllowed ? '禁止学生共享' : '允许学生共享'}
              </button>
              <button onClick={() => void toggleChat()}>
                {room?.chatEnabled ? '关闭学生聊天' : '开启学生聊天'}
              </button>
            </>
          )}

          {role === 'student' && recordingInProgress && (
            <button className="danger" onClick={requestStopRecording}>
              要求停止录屏
            </button>
          )}

          {remote && remote.status === 'active' && (
            <>
              <span className="badge warn">
                远程桌面：{remote.mode === 'control' ? '控制中' : '只看'}
              </span>
              {role === 'student' && agentCode && (
                <button onClick={() => setAgentCodeOpen(true)}>显示配对码</button>
              )}
              <button className="danger" onClick={() => void stopRemoteDesktop()}>
                停止远程桌面
              </button>
            </>
          )}

          <span className="muted small">
            {recorderState === 'recording'
              ? '录屏中（保存到本地）…'
              : recorderState === 'waiting_consent'
                ? '等待学生确认录屏'
                : recorderState === 'stopping'
                  ? '正在结束录屏…'
                  : ''}
          </span>
        </div>
      </div>

      <aside className="sidebar">
        <div>
          <div className="chat-head">
            <strong>课堂聊天</strong>
            <div className="participants">
              {participants
                .filter((item) => !item.leftAt)
                .map((item) => (
                  <span className="badge" key={item.id}>
                    {item.displayName}
                    {item.muted ? ' · 静音' : ''}
                  </span>
                ))}
            </div>
          </div>
          <div className="chat-list">
            {chatMessages.length === 0 && <p className="muted small">还没有消息。</p>}
            {chatMessages.map((message) => (
              <div
                className={`chat-item ${message.senderParticipantId === session.participantId ? 'self' : ''}`}
                key={message.id}
              >
                <div className="chat-meta">
                  <span>{participantLabel(participants, message.senderParticipantId)}</span>
                  <span>{new Date(message.createdAt).toLocaleTimeString()}</span>
                </div>
                <div className="chat-body">
                  {message.type === 'image' && message.attachmentId ? (
                    <>
                      {message.body && message.body !== message.attachment?.originalName && (
                        <p className="chat-caption">{message.body}</p>
                      )}
                      <a
                        href={apiRef.current?.attachmentUrl(roomId, message.attachmentId)}
                        target="_blank"
                        rel="noreferrer"
                      >
                        <img
                          src={apiRef.current?.attachmentUrl(roomId, message.attachmentId)}
                          alt={message.attachment?.originalName || message.body}
                        />
                      </a>
                    </>
                  ) : message.type === 'file' && message.attachmentId ? (
                    <>
                      {message.body && message.body !== message.attachment?.originalName && (
                        <p className="chat-caption">{message.body}</p>
                      )}
                      <a
                        href={apiRef.current?.attachmentUrl(roomId, message.attachmentId)}
                        target="_blank"
                        rel="noreferrer"
                      >
                        下载文件：{message.attachment?.originalName || message.body}
                      </a>
                    </>
                  ) : (
                    message.body
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="chat-compose">
          <form onSubmit={sendText} className="stack" style={{ gap: '0.5rem' }}>
            {pendingAttachment && (
              <div className="pending-attachment row between">
                <span className="small">
                  待发送：{pendingAttachment.originalName}
                </span>
                <button
                  type="button"
                  className="ghost small"
                  onClick={() => setPendingAttachment(null)}
                  disabled={uploading}
                >
                  移除
                </button>
              </div>
            )}
            <textarea
              value={chatText}
              onChange={(event) => setChatText(event.target.value)}
              onKeyDown={onChatKeyDown}
              placeholder={
                canSendChat ? '输入消息，Enter 发送，Shift+Enter 换行' : '老师已关闭学生聊天'
              }
              disabled={!canSendChat || ended}
            />
            <div className="row between">
              <label className="badge" style={{ margin: 0, cursor: ended ? 'not-allowed' : 'pointer' }}>
                {uploading ? '上传中…' : '图片/文件'}
                <input
                  type="file"
                  style={{ display: 'none' }}
                  onChange={(event) => void uploadFile(event)}
                  disabled={uploading || ended || !canSendChat}
                />
              </label>
              <button
                className="primary"
                type="submit"
                disabled={(!chatText.trim() && !pendingAttachment) || uploading || !canSendChat || ended}
              >
                发送
              </button>
            </div>
          </form>
        </div>
      </aside>

      {remoteModalOpen && role === 'teacher' && (
        <div className="modal-backdrop">
          <div className="modal stack">
            <h2 style={{ margin: 0 }}>远程桌面</h2>
            <p style={{ margin: 0 }} className="muted">
              请求学生启动远程协助 Agent 独立采集桌面。只看模式不发送鼠标键盘；控制模式可注入鼠标键盘。
            </p>
            <div className="row" style={{ justifyContent: 'flex-end' }}>
              <button onClick={() => setRemoteModalOpen(false)}>取消</button>
              <button onClick={() => void requestRemoteDesktop('view_only')}>请求只看</button>
              <button className="primary" onClick={() => void requestRemoteDesktop('control')}>
                请求控制
              </button>
            </div>
          </div>
        </div>
      )}

      {remoteRequest && role === 'student' && (
        <div className="modal-backdrop">
          <div className="modal stack">
            <h2 style={{ margin: 0 }}>远程桌面请求</h2>
            <p style={{ margin: 0 }}>
              {remoteRequest.teacherName} 请求查看你的桌面
              {remoteRequest.mode === 'control' ? '并希望控制鼠标和键盘' : ''}。
            </p>
            <p className="muted small" style={{ margin: 0 }}>
              同意后请启动 Windows 远程协助 Agent 并输入配对码。屏幕由 Agent 独立采集上传，无需在浏览器中共享。
            </p>
            {remoteError && <div className="error">{remoteError}</div>}
            <div className="row" style={{ justifyContent: 'flex-end' }}>
              <button onClick={() => void declineRemoteRequest()}>拒绝</button>
              <button onClick={() => void acceptRemoteRequest('view_only')}>允许只看</button>
              {remoteRequest.mode === 'control' && (
                <button className="primary" onClick={() => void acceptRemoteRequest('control')}>
                  允许控制
                </button>
              )}
            </div>
            <a className="small" href="/downloads/ppclass-agent-setup.exe" target="_blank" rel="noreferrer">
              下载远程协助 Agent 安装包（Windows）
            </a>
          </div>
        </div>
      )}

      {agentCode && agentCodeOpen && role === 'student' && (
        <div className="modal-backdrop">
          <div className="modal stack">
            <h2 style={{ margin: 0 }}>远程协助 Agent 配对码</h2>
            <div className="agent-code">{agentCode.code}</div>
            <p style={{ margin: 0 }}>
              启动 Windows 托盘的 PPClass 远程协助 Agent，点击“输入配对码”，粘贴上面的配对码。
            </p>
            <p className="muted small" style={{ margin: 0 }}>
              配对码有效期到 {new Date(agentCode.expiresAt).toLocaleTimeString()}。Agent 会独立采集并上传屏幕；未启动 Agent 时老师看不到屏幕。
            </p>
            <div className="row" style={{ justifyContent: 'flex-end' }}>
              <a className="badge" href="/downloads/ppclass-agent-setup.exe" target="_blank" rel="noreferrer">
                下载安装包
              </a>
              <button
                onClick={() => {
                  void navigator.clipboard?.writeText(agentCode.code);
                }}
              >
                复制配对码
              </button>
              <button className="primary" onClick={() => setAgentCodeOpen(false)}>
                知道了
              </button>
            </div>
          </div>
        </div>
      )}

      {consentRequest && (
        <div className="modal-backdrop">
          <div className="modal stack">
            <h2 style={{ margin: 0 }}>录屏请求</h2>
            <p style={{ margin: 0 }}>
              {consentRequest.teacherName} 请求录制本节课。录屏将包含教师屏幕、教师麦克风和你的声音。
            </p>
            <p className="muted small" style={{ margin: 0 }}>
              请在有效期内确认；拒绝后老师无法录制。
            </p>
            <div className="row" style={{ justifyContent: 'flex-end' }}>
              <button onClick={() => void declineConsent()}>拒绝</button>
              <button className="primary" onClick={() => void acceptConsent()}>
                同意录屏
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}







