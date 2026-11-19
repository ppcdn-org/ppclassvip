import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import ChangePasswordModal from '../components/ChangePasswordModal';
import {
  ApiError,
  PpclassApi,
  clearTeacherToken,
  listTeacherNicknames,
  loadTeacherNickname,
  loadTeacherToken,
  saveRoomSession,
  saveTeacherNickname,
  type Classroom,
} from '../sdk';

const stateLabel: Record<string, string> = {
  created: '未开始',
  waiting: '等待学生',
  active: '上课中',
  ended: '已结束',
  failed: '失败',
  expired: '已过期',
};

export default function TeacherHomePage() {
  const navigate = useNavigate();
  const token = loadTeacherToken();
  const api = useMemo(() => {
    const client = new PpclassApi();
    client.setToken(token || null);
    return client;
  }, [token]);

  const [rooms, setRooms] = useState<Classroom[]>([]);
  const [nickname, setNickname] = useState(loadTeacherNickname());
  const savedNicknames = listTeacherNicknames();
  const [title, setTitle] = useState('1v1 课堂');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [invite, setInvite] = useState<{ roomId: number; link: string; expiresAt: string } | null>(null);

  const loadRooms = useCallback(async () => {
    if (!token) {
      navigate('/login');
      return;
    }
    try {
      await api.me();
      const result = await api.listClassrooms();
      setRooms(result.rooms);
    } catch (err) {
      if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
        clearTeacherToken();
        navigate('/login');
        return;
      }
      setError(err instanceof Error ? err.message : '加载课堂失败');
    } finally {
      setLoading(false);
    }
  }, [api, navigate, token]);

  useEffect(() => {
    void loadRooms();
  }, [loadRooms]);

  async function createRoom(event: React.FormEvent) {
    event.preventDefault();
    setBusy('create');
    setError('');
    try {
      const result = await api.createClassroom(title.trim() || '1v1 课堂');
      setRooms((current) => [result.room, ...current]);
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建课堂失败');
    } finally {
      setBusy('');
    }
  }

  async function enterRoom(room: Classroom) {
    const name = nickname.trim();
    if (!name) {
      setError('请先填写你的昵称，学生会在课堂里看到这个名字。');
      return;
    }
    setBusy(`join-${room.id}`);
    setError('');
    try {
      saveTeacherNickname(name);
      const joined = await api.teacherJoin(room.id, name);
      saveRoomSession({
        roomId: room.id,
        roomToken: joined.roomToken,
        participantId: joined.participant.id,
        role: 'teacher',
        displayName: joined.participant.displayName,
        expiresAt: joined.expiresAt,
      });
      navigate(`/classroom/${room.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : '进入课堂失败');
    } finally {
      setBusy('');
    }
  }

  async function createInvite(room: Classroom) {
    const wasClosed = ['ended', 'failed', 'expired'].includes(room.state);
    setBusy(`invite-${room.id}`);
    setError('');
    setNotice('');
    try {
      const result = await api.createInvite(room.id);
      const link = `${window.location.origin}/join?invite=${encodeURIComponent(result.inviteToken)}`;
      setInvite({ roomId: room.id, link, expiresAt: result.expiresAt });
      await navigator.clipboard?.writeText(link).catch(() => undefined);
      if (wasClosed) {
        setNotice('课堂已重新开启，新的邀请链接已生成。');
        await loadRooms();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '生成邀请失败');
    } finally {
      setBusy('');
    }
  }

  async function endRoom(room: Classroom) {
    if (!window.confirm('确定结束这个课堂吗？')) return;
    setBusy(`end-${room.id}`);
    setError('');
    try {
      await api.endClassroom(room.id);
      await loadRooms();
    } catch (err) {
      setError(err instanceof Error ? err.message : '结束课堂失败');
    } finally {
      setBusy('');
    }
  }

  async function deleteRoom(room: Classroom) {
    if (
      !window.confirm(
        '确定删除这个课堂吗？删除后聊天记录、录屏和文件将一并清除，且不可恢复。',
      )
    ) {
      return;
    }
    setBusy(`delete-${room.id}`);
    setError('');
    setNotice('');
    try {
      await api.deleteClassroom(room.id);
      setRooms((current) => current.filter((item) => item.id !== room.id));
      if (invite?.roomId === room.id) setInvite(null);
      setNotice('课堂已删除。');
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除课堂失败');
    } finally {
      setBusy('');
    }
  }

  function logout() {
    clearTeacherToken();
    navigate('/login');
  }

  return (
    <div className="page">
      <div className="hero">
        <div>
          <h1>教师工作台</h1>
          <p className="muted" style={{ margin: 0 }}>
            创建课堂、生成邀请链接，然后进入课堂上课。
          </p>
        </div>
        <div className="row">
          <button className="ghost" onClick={() => setPasswordOpen(true)}>
            修改密码
          </button>
          <button className="ghost" onClick={logout}>
            退出登录
          </button>
        </div>
      </div>

      {error && <div className="error" style={{ marginBottom: '1rem' }}>{error}</div>}
      {notice && <div className="notice" style={{ marginBottom: '1rem' }}>{notice}</div>}

      <div className="card stack" style={{ marginBottom: '1.25rem' }}>
        <div>
          <label htmlFor="teacher-nickname">我的昵称</label>
          <input
            id="teacher-nickname"
            value={nickname}
            onChange={(event) => setNickname(event.target.value)}
            list="teacher-nickname-options"
            maxLength={32}
            placeholder="例如：王老师"
          />
          <datalist id="teacher-nickname-options">
            {savedNicknames.map((name) => (
              <option key={name} value={name} />
            ))}
          </datalist>
          <p className="muted small" style={{ margin: '0.35rem 0 0' }}>
            进入课堂时展示给学生的名字，会记住，下次可直接选择。
          </p>
        </div>
      </div>

      <div className="card stack" style={{ marginBottom: '1.25rem' }}>
        <form className="row" onSubmit={createRoom}>
          <div style={{ flex: 1, minWidth: 220 }}>
            <label htmlFor="room-title">课堂标题</label>
            <input
              id="room-title"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
              maxLength={120}
            />
          </div>
          <button className="primary" type="submit" disabled={busy === 'create'}>
            {busy === 'create' ? '创建中…' : '创建课堂'}
          </button>
        </form>
      </div>

      {loading ? (
        <p className="muted">加载中…</p>
      ) : rooms.length === 0 ? (
        <div className="card muted">还没有课堂，先创建一个。</div>
      ) : (
        <div className="grid-rooms">
          {rooms.map((room) => {
            const closed = ['ended', 'failed', 'expired'].includes(room.state);
            return (
              <div className="card stack room-card" key={room.id}>
                <div className="row between">
                  <h3>{room.title || '1v1 课堂'}</h3>
                  <span className={`badge ${room.state === 'active' ? 'live' : ''}`}>
                    {stateLabel[room.state] || room.state}
                  </span>
                </div>
                <div className="muted small">
                  房间号 {room.roomCode} · 创建于 {new Date(room.createdAt).toLocaleString()}
                </div>
                {room.recordingState && room.recordingState !== 'off' && (
                  <span className="badge recording">录屏：{room.recordingState}</span>
                )}
                <div className="row">
                  <button
                    className="primary"
                    disabled={busy === `join-${room.id}`}
                    onClick={() => void enterRoom(room)}
                  >
                    {closed ? '重新进入' : '进入课堂'}
                  </button>
                  <button
                    disabled={busy === `invite-${room.id}`}
                    onClick={() => void createInvite(room)}
                  >
                    {closed ? '重新生成邀请' : '生成邀请'}
                  </button>
                  {closed ? (
                    <button
                      className="danger"
                      disabled={busy === `delete-${room.id}`}
                      onClick={() => void deleteRoom(room)}
                    >
                      {busy === `delete-${room.id}` ? '删除中…' : '删除'}
                    </button>
                  ) : (
                    <button
                      className="danger"
                      disabled={busy === `end-${room.id}`}
                      onClick={() => void endRoom(room)}
                    >
                      结束
                    </button>
                  )}
                </div>
                {closed && (
                  <p className="muted small" style={{ margin: 0 }}>
                    该课堂已结束。点“重新进入”或“重新生成邀请”会重新开启同一个课堂（聊天记录保留）；点“删除”会永久清除该课堂及其聊天、录屏和文件。
                  </p>
                )}
                {invite?.roomId === room.id && (
                  <div className="invite-box">
                    <div className="small muted">
                      学生邀请链接（{new Date(invite.expiresAt).toLocaleString()} 前有效，一次性）
                    </div>
                    <input readOnly value={invite.link} onFocus={(event) => event.currentTarget.select()} />
                    <div className="row">
                      <button
                        onClick={() => {
                          void navigator.clipboard?.writeText(invite.link);
                        }}
                      >
                        复制链接
                      </button>
                      <button
                        onClick={() => {
                          window.open(invite.link, '_blank', 'noopener,noreferrer');
                        }}
                      >
                        打开邀请页
                      </button>
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
      <p className="footer-note">
        提示：先生成邀请并进入课堂，学生才能凭邀请加入。
      </p>
      {passwordOpen && (
        <ChangePasswordModal
          api={api}
          onClose={() => setPasswordOpen(false)}
          onChanged={() => setNotice('密码已修改，下次登录请使用新密码。')}
        />
      )}
    </div>
  );
}

