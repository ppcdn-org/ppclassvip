import { useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import {
  ApiError,
  PpclassApi,
  listStudentNicknames,
  loadStudentNickname,
  saveRoomSession,
  saveStudentNickname,
} from '../sdk';

export default function JoinPage() {
  const [search] = useSearchParams();
  const navigate = useNavigate();
  const inviteToken = search.get('invite') || '';
  const [nickname, setNickname] = useState(loadStudentNickname());
  const savedNicknames = listStudentNicknames();
  const [deviceStatus, setDeviceStatus] = useState('');
  const [checking, setChecking] = useState(false);
  const [joining, setJoining] = useState(false);
  const [error, setError] = useState('');

  async function checkDevices() {
    setChecking(true);
    setDeviceStatus('');
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: true });
      stream.getTracks().forEach((track) => track.stop());
      setDeviceStatus('摄像头和麦克风可用');
    } catch {
      setDeviceStatus('无法访问摄像头/麦克风，可继续以仅观看方式加入');
    } finally {
      setChecking(false);
    }
  }

  async function join(event: React.FormEvent) {
    event.preventDefault();
    if (!inviteToken) return;
    setJoining(true);
    setError('');
    try {
      const api = new PpclassApi();
      const result = await api.guestJoin(inviteToken, nickname.trim());
      saveStudentNickname(nickname.trim());
      saveRoomSession({
        roomId: result.room.id,
        roomToken: result.roomToken,
        participantId: result.participant.id,
        role: 'student',
        displayName: result.participant.displayName,
        expiresAt: result.expiresAt,
      });
      navigate(`/classroom/${result.room.id}`);
    } catch (err) {
      if (err instanceof ApiError && err.code === 'invite_already_used') {
        setError('邀请链接已被使用，请联系老师重新生成。');
      } else {
        setError(err instanceof Error ? err.message : '加入课堂失败');
      }
    } finally {
      setJoining(false);
    }
  }

  if (!inviteToken) {
    return (
      <div className="page narrow">
        <div className="card stack">
          <h1 style={{ margin: 0 }}>邀请无效</h1>
          <p className="muted">链接里没有邀请 token，请让老师重新生成邀请链接。</p>
          <Link to="/">返回首页</Link>
        </div>
      </div>
    );
  }

  return (
    <div className="page narrow">
      <div className="card stack">
        <div>
          <h1 style={{ margin: '0 0 0.3rem' }}>加入 1v1 课堂</h1>
          <p className="muted" style={{ margin: 0 }}>
            输入昵称后即可进入。加入时会请求摄像头和麦克风权限。
          </p>
        </div>
        <form className="stack" onSubmit={join}>
          <div>
            <label htmlFor="nickname">昵称</label>
            <input
              id="nickname"
              value={nickname}
              onChange={(event) => setNickname(event.target.value)}
              list="student-nickname-options"
              maxLength={32}
              placeholder="例如：小明"
              required
            />
            <datalist id="student-nickname-options">
              {savedNicknames.map((name) => (
                <option key={name} value={name} />
              ))}
            </datalist>
            {savedNicknames.length > 0 && (
              <p className="muted small" style={{ margin: '0.35rem 0 0' }}>
                可直接选择之前用过的昵称，或输入新的。
              </p>
            )}
          </div>
          <div className="row">
            <button type="button" onClick={() => void checkDevices()} disabled={checking}>
              {checking ? '检测中…' : '检测设备'}
            </button>
            {deviceStatus && <span className="muted small">{deviceStatus}</span>}
          </div>
          {error && <div className="error">{error}</div>}
          <button className="primary" type="submit" disabled={joining || !nickname.trim()}>
            {joining ? '加入中…' : '加入课堂'}
          </button>
        </form>
        <p className="footer-note">
          邀请链接为一次性使用；如果刷新页面后无法加入，请返回课堂入口或联系老师。
        </p>
        <p className="footer-note">
          需要被老师远程控制？请提前下载
          <a href="/downloads/ppclass-agent-setup.exe" target="_blank" rel="noreferrer">
            Windows 远程协助 Agent
          </a>
          ，进入课堂后输入配对码。
        </p>
      </div>
    </div>
  );
}
